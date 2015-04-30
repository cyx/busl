package broker

import (
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	. "github.com/heroku/busl/Godeps/_workspace/src/gopkg.in/check.v1"
	u "github.com/heroku/busl/util"
)

func Test(t *testing.T) { TestingT(t) }

type RegistrarSuite struct {
	registrar Registrar
	uuid      string
}

type BrokerSuite struct {
	registrar Registrar
	uuid      string
	writer    io.WriteCloser
	reader    io.ReadCloser
}

var _ = Suite(&RegistrarSuite{})
var _ = Suite(&BrokerSuite{})

/*
 * Registrar Suite
 */
func (s *RegistrarSuite) SetUpTest(c *C) {
	s.registrar = NewRedisRegistrar()
	s.uuid, _ = u.NewUUID()
}

func (s *RegistrarSuite) TestRegisteredIsRegistered(c *C) {
	s.registrar.Register(s.uuid)
	c.Assert(s.registrar.IsRegistered(s.uuid), Equals, true)
}

func (s *RegistrarSuite) TestUnregisteredIsNotRegistered(c *C) {
	c.Assert(s.registrar.IsRegistered(s.uuid), Equals, false)
}

func (s *RegistrarSuite) TestUnregisteredErrNotRegistered(c *C) {
	// Only complains for a Reader; existing behavior allows
	// publishers to create a channel on the fly.
	_, err := NewReader(s.uuid)
	c.Assert(err, Equals, ErrNotRegistered)

	// Writers can choose any ID it wants without consequence.
	_, err = NewWriter(s.uuid)
	c.Assert(err, Equals, ErrNotRegistered)
}

func (s *RegistrarSuite) TestRegisteredNoError(c *C) {
	s.registrar.Register(s.uuid)
	_, err := NewReader(s.uuid)
	c.Assert(err, IsNil)

	_, err = NewWriter(s.uuid)
	c.Assert(err, IsNil)
}

/*
 * Broker Suite
 */

func (s *BrokerSuite) SetUpTest(c *C) {
	s.registrar = NewRedisRegistrar()
	s.uuid, _ = u.NewUUID()
	s.registrar.Register(s.uuid)
	s.writer, _ = NewWriter(s.uuid)
	s.reader, _ = NewReader(s.uuid)
}

func readstring(r io.Reader) string {
	buf, _ := ioutil.ReadAll(r)
	return string(buf)
}

func (s *BrokerSuite) TestRedisSubscribe(c *C) {
	done := make(chan bool)
	go func() {
		c.Assert(readstring(s.reader), Equals, "busl")
		done <- true
	}()

	s.writer.Write([]byte("busl"))
	s.writer.Close()
	<-done
}

func (s *BrokerSuite) TestRedisSubscribeReplay(c *C) {
	s.writer.Write([]byte("busl"))
	s.writer.Close()
	c.Assert(readstring(s.reader), Equals, "busl")
}

func (s *BrokerSuite) TestRedisSubscribeWithOffset(c *C) {
	s.writer.Write([]byte("busl"))
	s.writer.Close()

	s.reader.(io.Seeker).Seek(2, 0)
	defer s.reader.Close()
	c.Assert(readstring(s.reader), Equals, "sl")
}

func (s *BrokerSuite) TestRedisSubscribeOffsetLimits(c *C) {
	s.writer.Write([]byte("busl"))
	s.writer.Close()

	s.reader.(io.Seeker).Seek(4, 0)
	defer s.reader.Close()
	c.Assert(readstring(s.reader), Equals, "")

	s.reader.(io.Seeker).Seek(5, 0)
	io.Copy(os.Stdout, s.reader)
}

func (s *BrokerSuite) TestRedisSubscribeConcurrent(c *C) {
	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		pub <- true
		s.writer.Write([]byte("busl"))
		s.writer.Close()
	}()

	go func() {
		<-pub
		c.Assert(readstring(s.reader), Equals, "busl")
		done <- true
	}()

	<-done
}

func (s *BrokerSuite) TestRedisReadFromClosed(c *C) {
	p := make([]byte, 10)

	s.reader.Read(p)
	s.writer.Write([]byte("hello"))
	s.writer.Close()

	// this read should short circuit with EOF
	_, err := s.reader.Read(p)
	c.Assert(err, Equals, io.EOF)

	// We'll get true here because r.closed is already set
	c.Assert(ReaderDone(s.reader), Equals, true)

	// We should still get true here because doneId is set
	r, _ := NewReader(s.uuid)
	c.Assert(ReaderDone(r), Equals, true)

	// Reader done on a regular io.Reader should return false
	// and not panic
	c.Assert(ReaderDone(strings.NewReader("hello")), Equals, false)

	// NoContent should respond accordingly based on offset
	c.Assert(NoContent(s.reader, 0), Equals, false)
	c.Assert(NoContent(s.reader, 5), Equals, true)
}
