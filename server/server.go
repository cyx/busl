package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/cyberdelia/pat"
	"github.com/heroku/busl/assets"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
)

func mkstream(w http.ResponseWriter, _ *http.Request) {
	registrar := broker.NewRedisRegistrar()
	uuid, err := util.NewUUID()
	if err != nil {
		log.Printf("%v", err)
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		return
	}

	if err := registrar.Register(uuid); err != nil {
		log.Printf("%v", err)
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		return
	}

	io.WriteString(w, string(uuid))
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

func pub(w http.ResponseWriter, r *http.Request) {
	uuid := util.UUID(r.URL.Query().Get(":uuid"))

	if !util.StringSliceUtil(r.TransferEncoding).Contains("chunked") {
		http.Error(w, "A chunked Transfer-Encoding header is required.", http.StatusBadRequest)
		return
	}

	msgBroker := broker.NewRedisBroker(uuid)
	defer msgBroker.UnsubscribeAll()

	bodyBuffer := bufio.NewReader(r.Body)
	defer r.Body.Close()

	buffer := make([]byte, 4096)

	for {
		readLen, err := bodyBuffer.Read(buffer)

		if readLen > 0 {
			msg := make([]byte, readLen)
			copy(msg, buffer[:readLen])
			msgBroker.Publish(msg)
		} else {
			return
		}

		switch {
		case err == io.ErrUnexpectedEOF:
			util.CountWithData("server.pub.read.eoferror", 1, "msg=\"%v\"", err.Error())
			return
		case err == io.EOF:
			return
		case err != nil:
			log.Printf("%#v", err)
			http.Error(w, "Unhandled error, please try again.", http.StatusInternalServerError)
			return
		}
	}
}

func sub(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	closeNotifier := w.(http.CloseNotifier).CloseNotify()

	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	uuid := util.UUID(r.URL.Query().Get(":uuid"))

	msgBroker := broker.NewRedisBroker(uuid)
	ch, err := msgBroker.Subscribe()
	defer msgBroker.Unsubscribe(ch)

	if err != nil {
		message := "Channel is not registered."
		if r.Header.Get("Accept") == "text/ascii; version=feral" {
			message = assets.HttpCatGone
		}

		http.Error(w, message, http.StatusGone)
		f.Flush()
		return
	}

	timer := time.NewTimer(*util.HeartbeatDuration)
	defer timer.Stop()

	for {
		select {
		case msg, msgOk := <-ch:
			if msgOk {
				timer.Reset(*util.HeartbeatDuration)
				w.Write(msg)
				f.Flush()
				continue
			} else {
				timer.Stop()
				return
			}
		case t, tOk := <-timer.C:
			if tOk {
				util.Count("server.sub.keepAlive")
				w.Write(util.GetNullByte())
				f.Flush()
				timer.Reset(*util.HeartbeatDuration)
				continue
			} else {
				util.CountWithData("server.sub.keepAlive.failed", 1, "timer=%v timerChannel=%v", timer, t)
				timer.Stop()
				w.Write([]byte("Unable to keep connection alive."))
				f.Flush()
				return
			}
		case cn, cnOk := <-closeNotifier:
			if cn && cnOk {
				util.Count("server.sub.clientClosed")
				timer.Stop()
				return
			}
		}
	}

	f.Flush()
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

func Start() {
	p := pat.New()

	p.GetFunc("/health", addDefaultHeaders(health))
	p.PostFunc("/streams", addDefaultHeaders(mkstream))
	p.PostFunc("/streams/:uuid", addDefaultHeaders(pub))
	p.GetFunc("/streams/:uuid", addDefaultHeaders(sub))

	http.HandleFunc("/", enforceHTTPS(p.ServeHTTP))

	if err := http.ListenAndServe(":"+*util.HttpPort, nil); err != nil {
		panic(err)
	}
}
