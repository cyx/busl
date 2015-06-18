package shuttle

import (
	"bufio"
	"io"
	"time"

	"github.com/heroku/busl/Godeps/_workspace/src/github.com/rcrowley/go-metrics"
)

// LogLineReader performs the reading of lines from an io.ReadCloser, encapsulating
// lines into a LogLine and emitting them on outbox
type LogLineReader struct {
	outbox    chan<- LogLine
	linesRead metrics.Counter
}

// NewLogLineReader constructs a new reader with it's own Outbox.
func NewLogLineReader(o chan<- LogLine, m metrics.Registry) LogLineReader {
	return LogLineReader{
		outbox:    o,
		linesRead: metrics.GetOrRegisterCounter("lines.read", m),
	}
}

// ReadLogLines reads lines from the ReadCloser
func (rdr LogLineReader) ReadLogLines(input io.ReadCloser) error {
	rdrIo := bufio.NewReader(input)

	for {
		line, err := rdrIo.ReadBytes('\n')
		currentLogTime := time.Now()

		if err != nil {
			input.Close()
			return err
		}

		rdr.Enqueue(LogLine{line, currentLogTime})
	}
}

// Enqueue a single log line and increment the line counters
func (rdr LogLineReader) Enqueue(ll LogLine) {
	rdr.outbox <- ll
	rdr.linesRead.Inc(1)
}
