package server

import (
	"bytes"
	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var baseURL = *util.StorageBaseURL

func TestMkstream(t *testing.T) {
	req, _ := http.NewRequest("POST", "/streams", &bytes.Buffer{})
	res := httptest.NewRecorder()

	mkstream(res, req)

	if res.Code != 200 {
		t.Fatalf("Expected %d to equal 200", res.Code)
	}
	if len(res.Body.String()) != 32 {
		t.Fatalf("Expected len(body) == 32")
	}
}

func Test410(t *testing.T) {
	streamId, _ := util.NewUUID()
	req, _ := http.NewRequest("GET", "/streams/"+streamId, nil)
	res := httptest.NewRecorder()

	sub(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("Expected %s to equal %s", res.Code, http.StatusNotFound)
	}

	if strings.TrimSpace(res.Body.String()) != notRegistered {
		t.Fatalf("Expected %s to equal %s", res.Body.String(), notRegistered)
	}
}

func TestPubNotRegistered(t *testing.T) {
	streamId, _ := util.NewUUID()
	req, _ := http.NewRequest("POST", "/streams/"+streamId, &bytes.Buffer{})
	req.TransferEncoding = []string{"chunked"}
	res := httptest.NewRecorder()

	pub(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("Expected %d to equal %d", res.Code, http.StatusNotFound)
	}
}

func TestPubWithoutTransferEncoding(t *testing.T) {
	req, _ := http.NewRequest("POST", "/streams/1234", nil)
	res := httptest.NewRecorder()

	pub(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatal("Expected %d to equal %d", res.Code, http.StatusBadRequest)
	}

	if strings.TrimSpace(res.Body.String()) != chunkedEncodingRequired {
		t.Fatalf("Expected %s to equal %s", res.Body.String(), chunkedEncodingRequired)
	}
}

func TestPubSub(t *testing.T) {
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
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		done := make(chan bool)

		go func() {
			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			res, err = http.Get(url)
			defer res.Body.Close()
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			body, _ := ioutil.ReadAll(res.Body)

			if !reflect.DeepEqual(body, expected) {
				t.Fatalf("Expected %s to equal %s", body, expected)
			}

			done <- true
		}()

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req, _ = http.NewRequest("POST", url, bytes.NewReader(expected))
		req.TransferEncoding = []string{"chunked"}
		res, err = http.DefaultClient.Do(req)
		defer res.Body.Close()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		<-done

		// Read the whole response after the publisher has
		// completed. The mechanics of this is different in that
		// most of the content will be replayed instead of received
		// in chunks as they arrive.
		res, err = http.Get(server.URL + "/streams/" + uuid)
		defer res.Body.Close()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		body, _ := ioutil.ReadAll(res.Body)
		if !reflect.DeepEqual(body, expected) {
			t.Fatalf("Expected %s to equal %s", body, expected)
		}

	}
}

func TestPubSubSSE(t *testing.T) {
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
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		done := make(chan bool)

		// curl -XPOST -H "Transfer-Encoding: chunked" -d "hello" <url>/streams/<uuid>
		req, _ = http.NewRequest("POST", url, bytes.NewReader([]byte(testdata.input)))
		req.TransferEncoding = []string{"chunked"}
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		defer res.Body.Close()

		go func() {
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Add("Accept", "text/event-stream")
			req.Header.Add("Last-Event-Id", strconv.Itoa(testdata.offset))

			// curl <url>/streams/<uuid>
			// -- waiting for publish to arrive
			res, err = http.DefaultClient.Do(req)
			defer res.Body.Close()
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			body, _ := ioutil.ReadAll(res.Body)
			if string(body) != testdata.output {
				t.Fatalf("Expected %s to equal %s", body, testdata.output)
			}

			if len(body) == 0 {
				if res.StatusCode != http.StatusNoContent {
					t.Fatalf("Expected %d to be 204 No Content", res.StatusCode)
				}
			}

			done <- true
		}()

		<-done
	}
}

func TestPut(t *testing.T) {
	server := httptest.NewServer(app())
	defer server.Close()

	// uuid = curl -XPUT <url>/streams/1/2/3
	req, _ := http.NewRequest("PUT", server.URL+"/streams/1/2/3", nil)
	res, err := http.DefaultClient.Do(req)
	defer res.Body.Close()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("Expected %d to be 201 Created", res.StatusCode)
	}

	registrar := broker.NewRedisRegistrar()
	if !registrar.IsRegistered("1/2/3") {
		t.Fatalf("Expected channel 1/2/3 to be registered")
	}
}

func TestSubGoneWithBackend(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	body, _ := ioutil.ReadAll(res.Body)
	if string(body) != "hello world" {
		t.Fatalf("Expected %s to be `hello world`", body)
	}
}

func TestPutWithBackend(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected %d to be 200 OK", res.StatusCode)
	}

	if output := <-put; string(output) != "hello world" {
		t.Fatalf("Expected %s to be `hello world`", output)
	}
}

func TestAuthentication(t *testing.T) {
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
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Expected %d to be 401 Unauthorized")
			}
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
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if res.StatusCode != status[method] {
				t.Fatalf("Expected %d to be %d", res.StatusCode, status[method])
			}
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
