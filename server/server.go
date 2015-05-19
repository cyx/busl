package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/heroku/busl/Godeps/_workspace/src/github.com/braintree/manners"
	"github.com/heroku/busl/Godeps/_workspace/src/github.com/gorilla/mux"
	"github.com/heroku/busl/Godeps/_workspace/src/github.com/heroku/rollbar"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
)

var gracefulServer *manners.GracefulServer

func init() {
	gracefulServer = manners.NewServer()
	gracefulServer.InnerServer.ReadTimeout = *util.HttpReadTimeout
	gracefulServer.InnerServer.WriteTimeout = *util.HttpWriteTimeout
}

func mkstream(w http.ResponseWriter, _ *http.Request) {
	registrar := broker.NewRedisRegistrar()
	uuid, err := util.NewUUID()
	if err != nil {
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		rollbar.Error(rollbar.ERR, fmt.Errorf("unable to create new uuid for stream: %#v", err))
		util.CountWithData("mkstream.create.fail", 1, "error=%s", err)
		return
	}

	if err := registrar.Register(uuid); err != nil {
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		rollbar.Error(rollbar.ERR, fmt.Errorf("unable to register stream: %#v", err))
		util.CountWithData("mkstream.create.fail", 1, "error=%s", err)
		return
	}

	util.Count("mkstream.create.success")
	io.WriteString(w, string(uuid))
}

func put(w http.ResponseWriter, r *http.Request) {
	registrar := broker.NewRedisRegistrar()

	if err := registrar.Register(key(r)); err != nil {
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		rollbar.Error(rollbar.ERR, fmt.Errorf("unable to register stream: %#v", err))
		util.CountWithData("put.create.fail", 1, "error=%s", err)
		return
	}
	util.Count("put.create.success")
	w.WriteHeader(http.StatusCreated)
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

func pub(w http.ResponseWriter, r *http.Request) {
	if !util.StringSliceUtil(r.TransferEncoding).Contains("chunked") {
		http.Error(w, chunkedEncodingRequired, http.StatusBadRequest)
		return
	}

	writer, err := broker.NewWriter(key(r))
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer writer.Close()

	body := bufio.NewReader(r.Body)
	defer r.Body.Close()

	_, err = io.Copy(writer, body)

	if err == io.ErrUnexpectedEOF {
		util.CountWithData("server.pub.read.eoferror", 1, "msg=\"%v\"", err.Error())
		return
	}

	if err != nil {
		log.Printf("%#v", err)
		http.Error(w, "Unhandled error, please try again.", http.StatusInternalServerError)
		rollbar.Error(rollbar.ERR, fmt.Errorf("unhandled error: %#v", err))
		return
	}

	// Asynchronously upload the output to our defined storage backend.
	go storeOutput(key(r), requestURI(r))
}

func sub(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	rd, err := newReader(w, r)
	if rd != nil {
		defer rd.Close()
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	io.Copy(newWriteFlusher(w), rd)
}

func app() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/health", addDefaultHeaders(health))

	// Legacy endpoint for creating the uuid `key` for you.
	r.HandleFunc("/streams", auth(addDefaultHeaders(mkstream)))

	// New `key` design for allowing any kind of id to be decided
	// by the caller (in this case, it mirrors what we have in S3).
	r.HandleFunc("/streams/{key:.+}", addDefaultHeaders(sub)).Methods("GET")
	r.HandleFunc("/streams/{key:.+}", addDefaultHeaders(pub)).Methods("POST")
	r.HandleFunc("/streams/{key:.+}", auth(addDefaultHeaders(put))).Methods("PUT")

	return logRequest(enforceHTTPS(r.ServeHTTP))
}

func Start(port string, shutdown <-chan struct{}) {
	log.Printf("http.start.port=%s\n", port)
	http.Handle("/", app())
	go listenForShutdown(shutdown)

	if err := gracefulServer.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server.server error=%v", err)
	}
}

func listenForShutdown(shutdown <-chan struct{}) {
	log.Println("http.graceful.await")
	<-shutdown
	log.Println("http.graceful.shutdown")
	gracefulServer.InnerServer.SetKeepAlivesEnabled(false) // TODO: Remove after merge of https://github.com/braintree/manners/pull/22
	gracefulServer.Shutdown <- true
}
