package broker

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

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

func ExampleOverflowingBuffer() {
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
	go func() {
		n, _ := io.Copy(ioutil.Discard, r)
		fmt.Printf("%d", n)
		close(done)
	}()
	w.Close()
	<-done

	//Output:
	// 32769
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

func ExampleReadShortCircuitEOF() {
	r, _, _ := setupReaderDone()

	p := make([]byte, 10)
	_, err := r.Read(p)

	fmt.Println(err)

	//Output:
	// EOF
}

func ExampleReaderDoneOnDrainedReader() {
	r, _, _ := setupReaderDone()
	fmt.Println(ReaderDone(r))

	//Output:
	// true
}

func ExampleReaderDoneOnNewReader() {
	_, _, uuid := setupReaderDone()

	r, _ := NewReader(uuid)
	fmt.Println(ReaderDone(r))

	//Output:
	// true
}

func ExampleReaderDoneOnNonBrokerReader() {
	fmt.Println(ReaderDone(strings.NewReader("hello")))

	//Output:
	// false
}

func ExampleNoContentWithData() {
	r, _, _ := setupReaderDone()

	fmt.Println(NoContent(r, 0))

	//Output:
	// false
}

func ExampleNoContentWithoutData() {
	r, _, _ := setupReaderDone()

	fmt.Println(NoContent(r, 5))

	//Output:
	// true
}
