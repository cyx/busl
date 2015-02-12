package sse

import (
	"testing"
)

var (
	input = []string{
		"hello",
		"world",
		"!",
		"hello\nworld",
		"hello\n\nworld",
	}

	output = []string{
		"id: 5\ndata: hello\n\n",
		"id: 10\ndata: world\n\n",
		"id: 11\ndata: !\n\n",
		"id: 22\ndata: hello\ndata: world\n\n",
		"id: 34\ndata: hello\ndata: \ndata: world\n\n",
	}
)

func channels() (chan []byte, chan []byte) {
	in := make(chan []byte, 10)
	out := make(chan []byte, 10)

	return in, out
}

func TestInOut(t *testing.T) {
	in, out := channels()
	go Transform(0, in, out)
	defer close(in)

	for i, v := range output {
		in <- []byte(input[i])
		res := <-out

		if string(res) != v {
			assertEqual(t, string(res), v)
		}
	}
}

func TestEvenOffset(t *testing.T) {
	in, out := channels()
	go Transform(5, in, out)
	defer close(in)

	in <- []byte(input[0])

	for i, v := range output[1:] {
		in <- []byte(input[i+1])
		res := <-out

		assertEqual(t, string(res), v)
	}
}

func TestAwkwardOffsetOriginalChunks(t *testing.T) {
	in, out := channels()
	go Transform(3, in, out)
	defer close(in)

	in <- []byte(input[0])
	res := <-out

	assertEqual(t, string(res), string(format(3, []byte("lo"))))
}

func TestAwkwardOffsetBigChunk(t *testing.T) {
	in, out := channels()
	go Transform(7, in, out)
	defer close(in)

	in <- []byte("hello world hola mundo")
	in <- []byte("good bye!")

	res1 := <-out
	res2 := <-out
	assertEqual(t, string(res1), string(format(7, []byte("orld hola mundo"))))
	assertEqual(t, string(res2), string(format(22, []byte("good bye!"))))
}

func assertEqual(t *testing.T, a, b string) {
	if a != b {
		t.Fatalf("\n\n\t\t%q != \n\t\t%q", a, b)
	}
}
