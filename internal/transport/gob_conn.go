package transport

// GOB DOCS: https://pkg.go.dev/encoding/gob

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

// NewGobConn Creates a new Gob connection using the Go standard connection library
func NewGobConn(conn net.Conn) *GobConn {
	return &GobConn{
		conn:    conn,
		encoder: gob.NewEncoder(conn),
		decoder: gob.NewDecoder(conn),
	}
}

// Send uses Gob to send a packet of type protocol.Frame
func (c *GobConn) Send(frame protocol.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.encoder.Encode(frame)
}

// Receive decodes the next Gob frame from the remote.
func (c *GobConn) Receive() (protocol.Frame, error) {
	var frame protocol.Frame
	err := c.decoder.Decode(&frame)
	if err != nil {
		return protocol.Frame{}, err
	}

	return frame, nil
}

// Close kills the underlying network connection.
func (c *GobConn) Close() error {
	return c.conn.Close()
}

// RawConn exposes the raw net.Conn for callers that need it.
func (c *GobConn) RawConn() net.Conn {
	return c.conn
}
