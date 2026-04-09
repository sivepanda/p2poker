package transport

import (
	"encoding/gob"
	"net"
	"sync"

	"github.com/sivepanda/p2poker/internal/protocol"
)

type GobConn struct {
	conn    net.Conn
	encoder *gob.Encoder
	decoder *gob.Decoder
	writeMu sync.Mutex
}

func NewGobConn(conn net.Conn) *GobConn {
	return &GobConn{
		conn:    conn,
		encoder: gob.NewEncoder(conn),
		decoder: gob.NewDecoder(conn),
	}
}

func (c *GobConn) Send(frame protocol.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.encoder.Encode(frame)
}

func (c *GobConn) Receive() (protocol.Frame, error) {
	var frame protocol.Frame
	err := c.decoder.Decode(&frame)
	if err != nil {
		return protocol.Frame{}, err
	}

	return frame, nil
}

func (c *GobConn) Close() error {
	return c.conn.Close()
}

func (c *GobConn) RawConn() net.Conn {
	return c.conn
}
