package sse

import (
	"bytes"
	"fmt"
)

const (
	id   = "id: %d\n"
	data = "data: %s\n"
)

var (
	newline = []byte{'\n'}
)

// Usage:
//
//     in := make(chan []byte, 10)
//     out := make(chan []byte, 10)
//     sse.Transform(in, out)
//
//     in <- "hello"
//     <-out // id: 0\ndata: hello\n\n
//
func Transform(offset int, in, out chan []byte) {
	for {
		msg, msgOk := <-in

		if msgOk {
			out <- format(offset, msg)
			offset += len(msg)
		} else {
			close(out)
			return
		}
	}
}

func format(pos int, msg []byte) []byte {
	buf := bytes.NewBufferString(fmt.Sprintf(id, pos+len(msg)))

	for _, line := range bytes.Split(msg, newline) {
		buf.WriteString(fmt.Sprintf(data, line))
	}

	buf.Write(newline)

	return buf.Bytes()
}
