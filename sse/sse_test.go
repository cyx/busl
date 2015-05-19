package sse

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

type table struct {
	offset int64
	input  string
	output string
}

var (
	testdata = []table{
		{0, "hello", "id: 5\ndata: hello\n\n"},
		{0, "hello\n", "id: 6\ndata: hello\ndata: \n\n"},
		{0, "hello\nworld", "id: 11\ndata: hello\ndata: world\n\n"},
		{0, "hello\nworld\n", "id: 12\ndata: hello\ndata: world\ndata: \n\n"},
		{1, "hello\nworld\n", "id: 12\ndata: ello\ndata: world\ndata: \n\n"},
		{6, "hello\nworld\n", "id: 12\ndata: world\ndata: \n\n"},
		{11, "hello\nworld\n", "id: 12\ndata: \ndata: \n\n"},
		{12, "hello\nworld\n", ""},
	}
)

func TestNoNewline(t *testing.T) {
	for _, data := range testdata {
		r := strings.NewReader(data.input)
		enc := NewEncoder(r)
		enc.(io.Seeker).Seek(data.offset, 0)

		if encoded := readstring(enc); encoded != data.output {
			t.Fatalf("Expected %s to equal %s", encoded, data.output)
		}
	}
}

func TestNonSeekableReader(t *testing.T) {
	// Seek the underlying reader before
	// passing to LimitReader: comparably similar
	// to scenario when reading from an http.Response
	r := strings.NewReader("hello world")
	r.Seek(10, 0)

	// Use LimitReader to hide the Seeker interface
	lr := io.LimitReader(r, 11)

	enc := NewEncoder(lr)
	enc.(io.Seeker).Seek(10, 0)

	// `id` should be 11 even though the underlying
	// reader wasn't seeked at all.
	if encoded := readstring(enc); encoded != "id: 11\ndata: d\n\n" {
		t.Fatalf(`Expected %s to be id: 11\ndata: d\n\n`)
	}
}

func readstring(r io.Reader) string {
	buf, _ := ioutil.ReadAll(r)
	return string(buf)
}
