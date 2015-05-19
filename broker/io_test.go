package broker

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/heroku/busl/util"
)

func setup() string {
	registrar := NewRedisRegistrar()
	uuid, _ := util.NewUUID()
	registrar.Register(uuid)

	return uuid
}

func ExamplePubSub() {
	uuid := setup()

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, r)
		done <- true
	}()

	go func() {
		pub <- true

		w, _ := NewWriter(uuid)
		w.Write([]byte("busl"))
		w.Write([]byte(" hello"))
		w.Write([]byte(" world"))
		w.Close()
	}()

	<-done

	//Output:
	// busl hello world
}

func ExampleFullReplay() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	buf := make([]byte, 16)
	io.ReadAtLeast(r, buf, 16)

	fmt.Printf("%s", buf)

	//Output:
	// busl hello world
}

func ExampleSeekCorrect() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))
	w.Close()

	r, _ := NewReader(uuid)
	r.(io.Seeker).Seek(10, 0)
	defer r.(io.Closer).Close()

	buf, _ := ioutil.ReadAll(r)
	fmt.Printf("%s", buf)

	//Output:
	// world
}

func ExampleSeekBeyond() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))
	w.Close()

	r, _ := NewReader(uuid)
	r.(io.Seeker).Seek(16, 0)
	defer r.Close()

	buf, _ := ioutil.ReadAll(r)
	fmt.Printf("%s", buf)

	//Output:
	//
}

func ExampleHalfReplayHalfSubscribed() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))

	r, _ := NewReader(uuid)

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, r)
		done <- true
	}()

	go func() {
		pub <- true

		w.Write([]byte(" hello"))
		w.Write([]byte(" world"))
		w.Close()
	}()

	<-done

	//Output:
	// busl hello world
}

func TestOverflowingBuffer(t *testing.T) {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write(bytes.Repeat([]byte("0"), 4096))
	w.Write(bytes.Repeat([]byte("1"), 4096))
	w.Write(bytes.Repeat([]byte("2"), 4096))
	w.Write(bytes.Repeat([]byte("3"), 4096))
	w.Write(bytes.Repeat([]byte("4"), 4096))
	w.Write(bytes.Repeat([]byte("5"), 4096))
	w.Write(bytes.Repeat([]byte("6"), 4096))
	w.Write(bytes.Repeat([]byte("7"), 4096))
	w.Write(bytes.Repeat([]byte("A"), 1))

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	done := make(chan struct{})
	var n int64
	go func() {
		n, _ = io.Copy(ioutil.Discard, r)
		close(done)
	}()
	w.Close()
	<-done

	if n != 32769 {
		t.Fatalf("Expected io.Copy to have written 32769 bytes")
	}
}

func setupReaderDone() (io.ReadCloser, io.WriteCloser, string) {
	p := make([]byte, 10)

	uuid := setup()

	r, _ := NewReader(uuid)
	r.Read(p)

	w, _ := NewWriter(uuid)
	w.Write([]byte("hello"))
	w.Close()

	return r, w, uuid
}

func TestReadShortCircuitEOF(t *testing.T) {
	r, _, _ := setupReaderDone()

	p := make([]byte, 10)

	if _, err := r.Read(p); err != io.EOF {
		t.Fatalf("Expected err to be EOF, got %v", err)
	}
}

func TestReaderDoneOnDrainedReader(t *testing.T) {
	r, _, _ := setupReaderDone()
	if !ReaderDone(r) {
		t.Fatalf("Expected reader to be done")
	}
}

func TestReaderDoneOnNewReader(t *testing.T) {
	_, _, uuid := setupReaderDone()

	if r, _ := NewReader(uuid); !ReaderDone(r) {
		t.Fatalf("Expected a new reader instance to report done")
	}
}

func TestReaderDoneOnNonBrokerReader(t *testing.T) {
	if ReaderDone(strings.NewReader("hello")) != false {
		t.Fatalf("Expected a non io.Reader to return false on `ReaderDone`")
	}
}

func TestNoContentWithData(t *testing.T) {
	r, _, _ := setupReaderDone()

	if NoContent(r, 0) != false {
		t.Fatalf("Expected a reader with readable offset to have content")
	}
}

func TestNoContentWithoutData(t *testing.T) {
	r, _, _ := setupReaderDone()

	if NoContent(r, 5) != true {
		t.Fatalf("Expected a reader with unreadable offset to have no content")
	}
}
