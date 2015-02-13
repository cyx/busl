package server

import (
	"bytes"
	"fmt"
	"github.com/heroku/busl/broker"
	. "github.com/heroku/busl/util"
	. "gopkg.in/check.v1"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func Test(t *testing.T) { TestingT(t) }

type HttpServerSuite struct{}

var _ = Suite(&HttpServerSuite{})
var sf = fmt.Sprintf

func newRequest(method, url, body string) *http.Request {
	return newRequestFromReader(method, url, bytes.NewBufferString(body))
}

func newRequestFromReader(method, url string, reader io.Reader) *http.Request {
	request, _ := http.NewRequest(method, url, reader)
	urlParts := strings.Split(url, "/")
	if method == "POST" {
		request.TransferEncoding = []string{"chunked"}
		request.Header.Add("Transfer-Encoding", "chunked")
	}
	if len(urlParts) == 3 {
		streamId := urlParts[2]
		setStreamId(request, streamId)
	}
	return request
}

func setStreamId(req *http.Request, streamId string) {
	req.URL.RawQuery = "%3Auuid=" + streamId + "&"
}

func (s *HttpServerSuite) TestMkstream(c *C) {
	request := newRequest("POST", "/streams", "")
	response := httptest.NewRecorder()

	mkstream(response, request)

	c.Assert(response.Code, Equals, 200)
	c.Assert(response.Body.String(), HasLen, 32)
}

func (s *HttpServerSuite) TestPub(c *C) {
	request := newRequest("POST", "/streams/1234", "")
	response := httptest.NewRecorder()

	pub(response, request)

	c.Assert(response.Code, Equals, http.StatusOK)
	c.Assert(response.Body.String(), IsEmptyString)
}

func (s *HttpServerSuite) TestPubWithoutTransferEncoding(c *C) {
	request, _ := http.NewRequest("POST", "/streams/1234", nil)
	setStreamId(request, "1234")
	response := httptest.NewRecorder()

	pub(response, request)

	c.Assert(response.Code, Equals, http.StatusBadRequest)
	c.Assert(response.Body.String(), Equals, "A chunked Transfer-Encoding header is required.\n")
}

func (s *HttpServerSuite) TestSub(c *C) {
	streamId, _ := NewUUID()
	registrar := broker.NewRedisRegistrar()
	registrar.Register(streamId)
	publisher := broker.NewRedisBroker(streamId)

	request := newRequest("GET", sf("/streams/%s", streamId), "")
	response := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	waiter := TimeoutFunc(time.Millisecond*5, func() {
		sub(response, request)
	})

	publisher.Write([]byte("busl1"))
	publisher.UnsubscribeAll()
	<-waiter

	c.Assert(response.Code, Equals, http.StatusOK)
	c.Assert(response.Body.String(), Equals, "busl1")
}

func (s *HttpServerSuite) TestPubSub(c *C) {
	streamId, _ := NewUUID()
	registrar := broker.NewRedisRegistrar()
	registrar.Register(streamId)

	body := new(bytes.Buffer)
	bodyCloser := ioutil.NopCloser(body)

	pubRequest := newRequestFromReader("POST", sf("/streams/%s", streamId), bodyCloser)
	pubResponse := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	pubBlocker := TimeoutFunc(time.Millisecond*5, func() {
		pub(pubResponse, pubRequest)
	})

	subRequest := newRequest("GET", sf("/streams/%s", streamId), "")
	subResponse := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	subBlocker := TimeoutFunc(time.Millisecond*5, func() {
		sub(subResponse, subRequest)
	})

	for _, m := range []string{"first", " ", "second", " ", "third"} {
		body.Write([]byte(m))
	}

	bodyCloser.Close()
	<-pubBlocker
	<-subBlocker

	c.Assert(subResponse.Code, Equals, http.StatusOK)
	c.Assert(subResponse.Body.String(), Equals, "first second third")
}

func (s *HttpServerSuite) TestBinaryPubSub(c *C) {
	streamId, _ := NewUUID()
	registrar := broker.NewRedisRegistrar()
	registrar.Register(streamId)

	body := new(bytes.Buffer)
	bodyCloser := ioutil.NopCloser(body)

	pubRequest := newRequestFromReader("POST", sf("/streams/%s", streamId), bodyCloser)
	pubResponse := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	pubBlocker := TimeoutFunc(time.Millisecond*5, func() {
		pub(pubResponse, pubRequest)
	})

	subRequest := newRequest("GET", sf("/streams/%s", streamId), "")
	subResponse := CloseNotifierRecorder{httptest.NewRecorder(), make(chan bool, 1)}

	subBlocker := TimeoutFunc(time.Millisecond*5, func() {
		sub(subResponse, subRequest)
	})

	expected := []byte{0x1f, 0x8b, 0x08, 0x00, 0x3f, 0x6b, 0xe1, 0x53, 0x00, 0x03, 0xed, 0xce, 0xb1, 0x0a, 0xc2, 0x30}
	for _, m := range expected {
		body.Write([]byte{m})
	}

	bodyCloser.Close()
	<-pubBlocker
	<-subBlocker

	c.Assert(subResponse.Code, Equals, http.StatusOK)
	c.Assert(subResponse.Body.Bytes(), DeepEquals, expected)
}

func (s *HttpServerSuite) TestSubWaitingPub(c *C) {
	contentType := "application/x-www-form-urlencoded"

	// Start the server in a randomly assigned port
	server := httptest.NewServer(app())
	defer server.Close()

	// uuid = curl -XPOST <url>/streams
	resp, err := http.Post(server.URL+"/streams", contentType, nil)
	defer resp.Body.Close()
	c.Assert(err, Equals, nil)

	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, Equals, nil)

	// uuid extracted
	uuid := string(body)
	c.Assert(len(uuid), Equals, 32)

	// curl <url>/streams/<uuid>
	// -- waiting for publish to arrive
	resp, err = http.Get(server.URL + "/streams/" + uuid)
	defer resp.Body.Close()
	c.Assert(err, Equals, nil)

	transport := &http.Transport{}
	client := &http.Client{Transport: transport}

	// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
	req := newRequestFromReader("POST", server.URL+"/streams/"+uuid, strings.NewReader("Hello"))
	r, err := client.Do(req)
	defer r.Body.Close()
	c.Assert(err, Equals, nil)

	// -- output grabbed from the earlier waiting subscribe.
	body, _ = ioutil.ReadAll(resp.Body)
	c.Assert(string(body), Equals, "Hello")
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
