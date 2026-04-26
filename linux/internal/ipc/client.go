//go:build linux

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const (
	// RequestTimeout is the default timeout per IPC request.
	RequestTimeout = 5 * time.Second
)

// Client connects to the IPC server and sends requests.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
	encoder *json.Encoder
}

// NewClient creates an IPC client (not yet connected).
func NewClient() *Client {
	return &Client{}
}

// Connect establishes a connection to the IPC server using the platform transport.
func (c *Client) Connect() error {
	conn, err := dialPlatform()
	if err != nil {
		return fmt.Errorf("ipc: client: connect: %w", err)
	}
	c.conn = conn
	c.scanner = bufio.NewScanner(conn)
	c.encoder = json.NewEncoder(conn)
	return nil
}

// Close closes the connection to the IPC server.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Send sends a request and waits for a response with a timeout.
func (c *Client) Send(req Request) (Response, error) {
	return c.SendContext(context.Background(), req)
}

// SendContext sends a request with context-based cancellation and timeout.
func (c *Client) SendContext(ctx context.Context, req Request) (Response, error) {
	if c.conn == nil {
		return Response{}, fmt.Errorf("ipc: client: not connected")
	}

	// Set deadline from context or default timeout.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(RequestTimeout)
	}
	c.conn.SetDeadline(deadline)

	if err := c.encoder.Encode(req); err != nil {
		return Response{}, fmt.Errorf("ipc: client: send: %w", err)
	}

	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return Response{}, fmt.Errorf("ipc: client: read: %w", err)
		}
		return Response{}, fmt.Errorf("ipc: client: connection closed")
	}

	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return Response{}, fmt.Errorf("ipc: client: decode: %w", err)
	}

	return resp, nil
}
