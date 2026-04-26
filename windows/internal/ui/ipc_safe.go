//go:build windows

package ui

import (
	"context"
	"sync"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// SafeIPCClient wraps an IPCClient with a mutex to make it safe for concurrent use.
// The underlying ipc.Client is not goroutine-safe (single conn/encoder/scanner),
// so all operations must be serialized.
type SafeIPCClient struct {
	mu     sync.Mutex
	client IPCClient
}

// NewSafeIPCClient wraps an IPCClient for concurrent use.
func NewSafeIPCClient(client IPCClient) *SafeIPCClient {
	return &SafeIPCClient{client: client}
}

// Connect establishes a connection to the IPC server.
func (s *SafeIPCClient) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Connect()
}

// Close closes the connection.
func (s *SafeIPCClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Close()
}

// SendContext sends a request with serialized access.
func (s *SafeIPCClient) SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.SendContext(ctx, req)
}
