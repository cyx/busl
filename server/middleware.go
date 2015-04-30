package server

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/heroku/busl/Godeps/_workspace/src/github.com/gorilla/mux"
	"github.com/heroku/busl/Godeps/_workspace/src/github.com/heroku/authenticater"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/sse"
	"github.com/heroku/busl/storage"
	"github.com/heroku/busl/util"
)

func enforceHTTPS(fn http.HandlerFunc) http.HandlerFunc {
	if !*util.EnforceHTTPS {
		return fn
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "https" {
			url := r.URL
			url.Host = r.Host
			url.Scheme = "https"

			http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
			return
		}

		fn(w, r)
	}
}

func auth(fn http.HandlerFunc) http.HandlerFunc {
	if *util.Creds == "" {
		return fn
	}

	if auth, err := authenticater.NewBasicAuthFromString(*util.Creds); err != nil {
		log.Fatalf("server.middleware error=%v", err)
		return nil
	} else {
		return authenticater.WrapAuth(auth, fn)
	}
}

func logRequest(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := util.NewResponseLogger(w, requestId(r))
		fn(logger, r)
		logger.WriteLog()
	}
}

func addDefaultHeaders(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		fn(w, r)
	}
}

func requestId(r *http.Request) (id string) {
	if id = r.Header.Get("Request-Id"); id == "" {
		id = r.Header.Get("X-Request-Id")
	}

	if id == "" {
		// In the event of a rare case where uuid
		// generation fails, it's probably more
		// desirable to continue as is with an empty
		// request_id than to bubble the error up the
		// stack.
		uuid, _ := util.NewUUID()
		id = string(uuid)
	}

	return id
}

func offset(r *http.Request) int64 {
	var off string

	if off = r.Header.Get("last-event-id"); off == "" {
		if val := r.Header.Get("Range"); val != "" {
			tuple := strings.SplitN(val, "-", 2)
			off = tuple[0]
		}
	}

	n, _ := strconv.Atoi(off)
	return int64(n)
}

// Given URL:
//   http://build-output.heroku.com/streams/1/2/3?foo=bar
//
// Returns:
//   1/2/3?foo=bar
func requestURI(r *http.Request) string {
	res := key(r)

	if r.URL.RawQuery != "" {
		res += "?" + r.URL.RawQuery
	}

	return res
}

func key(r *http.Request) string {
	return mux.Vars(r)["key"]
}

// Returns a broker or blob reader.
func newStorageReader(w http.ResponseWriter, r *http.Request) (io.ReadCloser, error) {
	// Get the offset from Last-Event-ID: or Range:
	offset := offset(r)

	rd, err := broker.NewReader(key(r))

	// Not cached in the broker anymore, try the storage backend as a fallback.
	if err == broker.ErrNotRegistered {
		return storage.Get(requestURI(r), offset)
	}

	if offset > 0 {
		if seeker, ok := rd.(io.Seeker); ok {
			seeker.Seek(offset, 0)
		}
	}
	return rd, err
}

func newReader(w http.ResponseWriter, r *http.Request) (io.ReadCloser, error) {
	rd, err := newStorageReader(w, r)
	if err != nil {
		if rd != nil {
			rd.Close()
		}
		return rd, err
	}

	// For default requests, we use a null byte for sending
	// the keepalive ack.
	ack := util.GetNullByte()

	if broker.NoContent(rd, offset(r)) {
		return nil, errNoContent
	}

	if r.Header.Get("Accept") == "text/event-stream" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		encoder := sse.NewEncoder(rd)
		encoder.(io.Seeker).Seek(offset(r), 0)

		rd = ioutil.NopCloser(encoder)

		// For SSE, we change the ack to a :keepalive
		ack = []byte(":keepalive\n")
	}

	done := w.(http.CloseNotifier).CloseNotify()
	return newKeepAliveReader(rd, ack, *util.HeartbeatDuration, done), nil
}

func storeOutput(channel string, requestURI string) {
	if buf, err := broker.Get(channel); err == nil {
		if err := storage.Put(requestURI, bytes.NewBuffer(buf)); err != nil {
			util.CountWithData("server.storeOutput.put.error", 1, "err=%s", err.Error())
		}
	} else {
		util.CountWithData("server.storeOutput.get.error", 1, "err=%s", err.Error())
	}
}
