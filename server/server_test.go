package server

import (
	"bytes"
	"fmt"
	. "github.com/heroku/busl/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type HttpServerSuite struct{}

var _ = Suite(&HttpServerSuite{})
var sf = fmt.Sprintf
var baseURL = *util.StorageBaseURL

func newRequest(method, url, body string) *http.Request {
	return newRequestFromReader(method, url, bytes.NewBufferString(body))
}

func newRequestFromReader(method, url string, reader io.Reader) *http.Request {
	request, _ := http.NewRequest(method, url, reader)
	if method == "POST" {
		request.TransferEncoding = []string{"chunked"}
		request.Header.Add("Transfer-Encoding", "chunked")
	}
	return request
}

func (s *HttpServerSuite) TestMkstream(c *C) {
	request := newRequest("POST", "/streams", "")
	response := httptest.NewRecorder()

	mkstream(response, request)

	c.Assert(response.Code, Equals, 200)
	c.Assert(response.Body.String(), HasLen, 32)
}

func (s *HttpServerSuite) Test410(c *C) {
	streamId, _ := util.NewUUID()
	request := newRequest("GET", "/streams/"+string(streamId), "")
	response := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	sub(response, request)

	c.Assert(response.Code, Equals, http.StatusNotFound)
	c.Assert(response.Body.String(), Equals, "Channel is not registered.\n")
}

func (s *HttpServerSuite) TestPubNotRegistered(c *C) {
	streamId, _ := util.NewUUID()
	request := newRequest("POST", "/streams/"+string(streamId), "")
	response := httptest.NewRecorder()

	pub(response, request)

	c.Assert(response.Code, Equals, http.StatusNotFound)
}

func (s *HttpServerSuite) TestPubWithoutTransferEncoding(c *C) {
	request, _ := http.NewRequest("POST", "/streams/1234", nil)
	response := httptest.NewRecorder()

	pub(response, request)

	c.Assert(response.Code, Equals, http.StatusBadRequest)
	c.Assert(response.Body.String(), Equals, "A chunked Transfer-Encoding header is required.\n")
}

func (s *HttpServerSuite) TestPubSub(c *C) {
	server := httptest.NewServer(app())
	defer server.Close()

	data := [][]byte{
		[]byte{'h', 'e', 'l', 'l', 'o'},
		[]byte{0x1f, 0x8b, 0x08, 0x00, 0x3f, 0x6b, 0xe1, 0x53, 0x00, 0x03, 0xed, 0xce, 0xb1, 0x0a, 0xc2, 0x30},
	}

	for _, expected := range data {
		// uuid = curl -XPOST <url>/streams
		resp, err := http.Post(server.URL+"/streams", "", nil)
		defer resp.Body.Close()
		c.Assert(err, Equals, nil)

		body, err := ioutil.ReadAll(resp.Body)
		c.Assert(err, Equals, nil)

		// uuid extracted
		uuid := string(body)
		c.Assert(len(uuid), Equals, 32)

		done := make(chan bool)

		go func() {
			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			resp, err = http.Get(server.URL + "/streams/" + uuid)
			defer resp.Body.Close()
			c.Assert(err, IsNil)

			body, _ = ioutil.ReadAll(resp.Body)
			c.Assert(body, DeepEquals, expected)

			done <- true
		}()

		transport := &http.Transport{}
		client := &http.Client{Transport: transport}

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req := newRequestFromReader("POST", server.URL+"/streams/"+uuid, bytes.NewReader(expected))
		r, err := client.Do(req)
		r.Body.Close()
		c.Assert(err, IsNil)

		<-done
	}
}

func (s *HttpServerSuite) TestPubSubSSE(c *C) {
	server := httptest.NewServer(app())
	defer server.Close()

	data := []struct {
		offset int
		input  string
		output string
	}{
		{0, "hello", "id: 5\ndata: hello\n\n"},
		{0, "hello\n", "id: 6\ndata: hello\ndata: \n\n"},
		{0, "hello\nworld", "id: 11\ndata: hello\ndata: world\n\n"},
		{0, "hello\nworld\n", "id: 12\ndata: hello\ndata: world\ndata: \n\n"},
		{1, "hello\nworld\n", "id: 12\ndata: ello\ndata: world\ndata: \n\n"},
		{6, "hello\nworld\n", "id: 12\ndata: world\ndata: \n\n"},
		{11, "hello\nworld\n", "id: 12\ndata: \ndata: \n\n"},
		{12, "hello\nworld\n", ""},
	}

	client := &http.Client{Transport: &http.Transport{}}

	for _, testdata := range data {
		uuid, _ := util.NewUUID()
		url := server.URL + "/streams/" + uuid

		// curl -XPUT <url>/streams/<uuid>
		request, _ := http.NewRequest("PUT", url, nil)
		resp, err := client.Do(request)
		defer resp.Body.Close()
		c.Assert(err, Equals, nil)

		done := make(chan bool)

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req := newRequestFromReader("POST", url, bytes.NewReader([]byte(testdata.input)))
		r, err := client.Do(req)
		c.Assert(err, Equals, nil)
		r.Body.Close()

		go func() {
			request, _ := http.NewRequest("GET", url, nil)
			request.Header.Add("Accept", "text/event-stream")
			request.Header.Add("Last-Event-Id", strconv.Itoa(testdata.offset))

			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			resp, err = client.Do(request)
			defer resp.Body.Close()
			c.Assert(err, IsNil)

			body, _ := ioutil.ReadAll(resp.Body)
			c.Assert(body, DeepEquals, []byte(testdata.output))

			if len(body) == 0 {
				c.Assert(resp.StatusCode, Equals, http.StatusNoContent)
			}

			done <- true
		}()

		<-done
	}
}

func (s *HttpServerSuite) TestPut(c *C) {
	server := httptest.NewServer(app())
	defer server.Close()

	transport := &http.Transport{}
	client := &http.Client{Transport: transport}

	// uuid = curl -XPUT <url>/streams/1/2/3
	request := newRequest("PUT", server.URL+"/streams/1/2/3", "")
	resp, err := client.Do(request)
	defer resp.Body.Close()
	c.Assert(err, Equals, nil)
	c.Assert(resp.StatusCode, Equals, http.StatusCreated)

	registrar := broker.NewRedisRegistrar()
	c.Assert(registrar.IsRegistered("1/2/3"), Equals, true)
}

func (s *HttpServerSuite) TestSubGoneWithBackend(c *C) {
	uuid, _ := util.NewUUID()

	storage, get, _ := fileServer(uuid)
	defer storage.Close()

	*util.StorageBaseURL = storage.URL
	defer func() {
		*util.StorageBaseURL = baseURL
	}()

	server := httptest.NewServer(app())
	defer server.Close()

	get <- []byte("hello world")

	resp, err := http.Get(server.URL + "/streams/" + uuid)
	defer resp.Body.Close()
	c.Assert(err, IsNil)

	body, _ := ioutil.ReadAll(resp.Body)
	c.Assert(body, DeepEquals, []byte("hello world"))
}

func (s *HttpServerSuite) TestPutWithBackend(c *C) {
	uuid, _ := util.NewUUID()

	storage, _, put := fileServer(uuid)
	defer storage.Close()

	*util.StorageBaseURL = storage.URL
	defer func() {
		*util.StorageBaseURL = baseURL
	}()

	server := httptest.NewServer(app())
	defer server.Close()

	transport := &http.Transport{}
	client := &http.Client{Transport: transport}

	registrar := broker.NewRedisRegistrar()
	registrar.Register(uuid)

	// uuid = curl -XPUT <url>/streams/1/2/3
	request := newRequest("POST", server.URL+"/streams/"+uuid, "hello world")
	resp, err := client.Do(request)
	defer resp.Body.Close()
	c.Assert(err, Equals, nil)
	c.Assert(resp.StatusCode, Equals, 200)
	c.Assert(<-put, DeepEquals, []byte("hello world"))
}

func (s *HttpServerSuite) TestAuthentication(c *C) {
	*util.Creds = "u:pass1|u:pass2"
	defer func() {
		*util.Creds = ""
	}()

	server := httptest.NewServer(app())
	defer server.Close()

	transport := &http.Transport{}
	client := &http.Client{Transport: transport}

	testdata := map[string]string{
		"POST": "/streams",
		"PUT":  "/streams/1/2/3",
	}

	status := map[string]int{
		"POST": http.StatusOK,
		"PUT":  http.StatusCreated,
	}

	// Validate that we return 401 for empty and invalid tokens
	for _, token := range []string{"", "invalid"} {
		for method, path := range testdata {
			request := newRequest(method, server.URL+path, "")
			if token != "" {
				request.SetBasicAuth("", token)
			}
			resp, err := client.Do(request)
			defer resp.Body.Close()
			c.Assert(err, Equals, nil)
			c.Assert(resp.Status, Equals, "401 Unauthorized")
		}
	}

	// Validate that all the colon separated token values are
	// accepted
	for _, token := range []string{"pass1", "pass2"} {
		for method, path := range testdata {
			request := newRequest(method, server.URL+path, "")
			request.SetBasicAuth("u", token)
			resp, err := client.Do(request)
			defer resp.Body.Close()
			c.Assert(err, Equals, nil)
			c.Assert(resp.StatusCode, Equals, status[method])
		}
	}
}

type CloseNotifierRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func (cnr CloseNotifierRecorder) close() {
	cnr.closed <- true
}

func (cnr CloseNotifierRecorder) CloseNotify() <-chan bool {
	return cnr.closed
}

func fileServer(id string) (*httptest.Server, chan []byte, chan []byte) {
	get := make(chan []byte, 10)
	put := make(chan []byte, 10)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+id, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.Write(<-get)
		case "PUT":
			b, _ := ioutil.ReadAll(r.Body)
			put <- b
		}
	})

	server := httptest.NewServer(mux)
	return server, get, put
}
