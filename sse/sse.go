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
	pos := 0

	for {
		msg, msgOk := <-in

		if msgOk {
			length := len(msg)
			if pos >= offset {
				out <- format(pos, msg)
			} else if pos < offset && offset < pos+length {
				out <- format(offset, msg[(offset-pos):])
			}
			pos += length
		} else {
			close(out)
			return
		}
	}
}

func format(pos int, msg []byte) []byte {
	buf := bytes.NewBufferString(fmt.Sprintf(id, pos))

	for _, line := range bytes.Split(msg, newline) {
		buf.WriteString(fmt.Sprintf(data, line))
	}

	buf.Write(newline)

	return buf.Bytes()
}
