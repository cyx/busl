package server

import (
	"bytes"
	"fmt"
	. "github.com/heroku/busl/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
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

func (s *HttpServerSuite) TestMkstream(c *C) {
	req, _ := http.NewRequest("POST", "/streams", &bytes.Buffer{})
	res := httptest.NewRecorder()

	mkstream(res, req)

	c.Assert(res.Code, Equals, 200)
	c.Assert(res.Body.String(), HasLen, 32)
}

func (s *HttpServerSuite) Test410(c *C) {
	streamId, _ := util.NewUUID()
	req, _ := http.NewRequest("GET", "/streams/"+streamId, nil)
	res := httptest.NewRecorder()

	sub(res, req)

	c.Assert(res.Code, Equals, http.StatusNotFound)
	c.Assert(res.Body.String(), Equals, "Channel is not registered.\n")
}

func (s *HttpServerSuite) TestPubNotRegistered(c *C) {
	streamId, _ := util.NewUUID()
	req, _ := http.NewRequest("POST", "/streams/"+streamId, &bytes.Buffer{})
	req.TransferEncoding = []string{"chunked"}
	res := httptest.NewRecorder()

	pub(res, req)

	c.Assert(res.Code, Equals, http.StatusNotFound)
}

func (s *HttpServerSuite) TestPubWithoutTransferEncoding(c *C) {
	req, _ := http.NewRequest("POST", "/streams/1234", nil)
	res := httptest.NewRecorder()

	pub(res, req)

	c.Assert(res.Code, Equals, http.StatusBadRequest)
	c.Assert(res.Body.String(), Equals, "A chunked Transfer-Encoding header is required.\n")
}

func (s *HttpServerSuite) TestPubSub(c *C) {
	server := httptest.NewServer(app())
	defer server.Close()

	data := [][]byte{
		[]byte{'h', 'e', 'l', 'l', 'o'},
		[]byte{0x1f, 0x8b, 0x08, 0x00, 0x3f, 0x6b, 0xe1, 0x53, 0x00, 0x03, 0xed, 0xce, 0xb1, 0x0a, 0xc2, 0x30},
		bytes.Repeat([]byte{'0'}, 32769),
	}

	for _, expected := range data {
		uuid, _ := util.NewUUID()
		url := server.URL + "/streams/" + uuid

		// uuid = curl -XPOST <url>/streams
		req, _ := http.NewRequest("PUT", url, nil)
		res, err := http.DefaultClient.Do(req)
		defer res.Body.Close()
		c.Assert(err, Equals, nil)

		done := make(chan bool)

		go func() {
			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			res, err = http.Get(url)
			defer res.Body.Close()
			c.Assert(err, IsNil)

			body, _ := ioutil.ReadAll(res.Body)
			c.Assert(body, DeepEquals, expected)

			done <- true
		}()

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req, _ = http.NewRequest("POST", url, bytes.NewReader(expected))
		req.TransferEncoding = []string{"chunked"}
		res, err = http.DefaultClient.Do(req)
		defer res.Body.Close()
		c.Assert(err, IsNil)

		<-done

		// Read the whole response after the publisher has
		// completed. The mechanics of this is different in that
		// most of the content will be replayed instead of received
		// in chunks as they arrive.
		resp, err = http.Get(server.URL + "/streams/" + uuid)
		defer resp.Body.Close()
		c.Assert(err, IsNil)

		body, _ = ioutil.ReadAll(resp.Body)
		c.Assert(body, DeepEquals, expected)
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

	for _, testdata := range data {
		uuid, _ := util.NewUUID()
		url := server.URL + "/streams/" + uuid

		// curl -XPUT <url>/streams/<uuid>
		req, _ := http.NewRequest("PUT", url, nil)
		res, err := http.DefaultClient.Do(req)
		defer res.Body.Close()
		c.Assert(err, Equals, nil)

		done := make(chan bool)

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req, _ = http.NewRequest("POST", url, bytes.NewReader([]byte(testdata.input)))
		req.TransferEncoding = []string{"chunked"}
		res, err = http.DefaultClient.Do(req)
		c.Assert(err, Equals, nil)
		defer res.Body.Close()

		go func() {
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Add("Accept", "text/event-stream")
			req.Header.Add("Last-Event-Id", strconv.Itoa(testdata.offset))

			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			res, err = http.DefaultClient.Do(req)
			defer res.Body.Close()
			c.Assert(err, IsNil)

			body, _ := ioutil.ReadAll(res.Body)
			c.Assert(body, DeepEquals, []byte(testdata.output))

			if len(body) == 0 {
				c.Assert(res.StatusCode, Equals, http.StatusNoContent)
			}

			done <- true
		}()

		<-done
	}
}

func (s *HttpServerSuite) TestPut(c *C) {
	server := httptest.NewServer(app())
	defer server.Close()

	// uuid = curl -XPUT <url>/streams/1/2/3
	req, _ := http.NewRequest("PUT", server.URL+"/streams/1/2/3", nil)
	res, err := http.DefaultClient.Do(req)
	defer res.Body.Close()
	c.Assert(err, Equals, nil)
	c.Assert(res.StatusCode, Equals, http.StatusCreated)

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

	res, err := http.Get(server.URL + "/streams/" + uuid)
	defer res.Body.Close()
	c.Assert(err, IsNil)

	body, _ := ioutil.ReadAll(res.Body)
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

	registrar := broker.NewRedisRegistrar()
	registrar.Register(uuid)

	// uuid = curl -XPUT <url>/streams/1/2/3
	req, _ := http.NewRequest("POST", server.URL+"/streams/"+uuid, bytes.NewReader([]byte("hello world")))
	req.TransferEncoding = []string{"chunked"}
	res, err := http.DefaultClient.Do(req)
	defer res.Body.Close()
	c.Assert(err, Equals, nil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(<-put, DeepEquals, []byte("hello world"))
}

func (s *HttpServerSuite) TestAuthentication(c *C) {
	*util.Creds = "u:pass1|u:pass2"
	defer func() {
		*util.Creds = ""
	}()

	server := httptest.NewServer(app())
	defer server.Close()

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
			req, _ := http.NewRequest(method, server.URL+path, nil)
			if token != "" {
				req.SetBasicAuth("", token)
			}
			res, err := http.DefaultClient.Do(req)
			defer res.Body.Close()
			c.Assert(err, Equals, nil)
			c.Assert(res.Status, Equals, "401 Unauthorized")
		}
	}

	// Validate that all the colon separated token values are
	// accepted
	for _, token := range []string{"pass1", "pass2"} {
		for method, path := range testdata {
			req, _ := http.NewRequest(method, server.URL+path, nil)
			req.SetBasicAuth("u", token)
			res, err := http.DefaultClient.Do(req)
			defer res.Body.Close()
			c.Assert(err, Equals, nil)
			c.Assert(res.StatusCode, Equals, status[method])
		}
	}
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
