//go:build linux

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// Handler processes an IPC request and returns a response.
type Handler func(Request) Response

// Server accepts IPC connections and dispatches requests to a handler.
type Server struct {
	mu       sync.Mutex
	handler  Handler
	listener Listener
	netLn    net.Listener
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	conns    []net.Conn
}

// NewServer creates an IPC server with the given platform listener.
func NewServer(listener Listener) *Server {
	return &Server{
		listener: listener,
	}
}

// SetHandler registers the request handler.
func (s *Server) SetHandler(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// Start begins accepting connections. It blocks until ctx is cancelled or Stop is called.
func (s *Server) Start(ctx context.Context) error {
	ln, err := s.listener.Listen()
	if err != nil {
		return fmt.Errorf("ipc: server: listen: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.netLn = ln
	s.cancel = cancel
	s.mu.Unlock()

	// Close listener and all active connections when context is cancelled.
	go func() {
		<-ctx.Done()
		ln.Close()
		s.closeAllConns()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.wg.Wait()
				return nil
			default:
				return fmt.Errorf("ipc: server: accept: %w", err)
			}
		}

		s.trackConn(conn)
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer s.untrackConn(c)
			defer c.Close()
			s.handleConnection(ctx, c)
		}(conn)
	}
}

// Stop halts the server and cleans up the transport.
func (s *Server) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.listener.Cleanup()
}

// trackConn adds a connection to the tracked list.
func (s *Server) trackConn(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns = append(s.conns, c)
}

// untrackConn removes a connection from the tracked list.
func (s *Server) untrackConn(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, conn := range s.conns {
		if conn == c {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return
		}
	}
}

// closeAllConns closes all tracked connections to unblock readers.
func (s *Server) closeAllConns() {
	s.mu.Lock()
	conns := make([]net.Conn, len(s.conns))
	copy(conns, s.conns)
	s.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
}

// handleConnection reads JSON messages line by line, dispatches to handler,
// and writes JSON responses.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	// Audit fix R-T1.1 (2026-05-04) — read SO_PEERCRED at accept time and
	// reject peers whose UID is not authorised to drive the service.
	// See peercred_linux.go for the policy.
	cred, credErr := getPeerCred(conn)
	encoder := json.NewEncoder(conn)
	if credErr == nil && !authorizePeer(cred) {
		fmt.Fprintf(os.Stderr, "SECURITY AUDIT: IPC peer rejected uid=%d gid=%d pid=%d\n",
			cred.UID, cred.GID, cred.PID)
		_ = encoder.Encode(Response{Status: StatusError, Error: "peer_unauthorized"})
		return
	}

	scanner := bufio.NewScanner(conn)
	// Limit max message size to 4KB to prevent resource exhaustion.
	scanner.Buffer(make([]byte, 4096), 4096)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := Response{Status: StatusError, Error: "invalid_json"}
			encoder.Encode(resp)
			continue
		}

		s.mu.Lock()
		h := s.handler
		s.mu.Unlock()

		if h == nil {
			resp := Response{Status: StatusError, Error: "no_handler"}
			encoder.Encode(resp)
			continue
		}

		resp := h(req)
		encoder.Encode(resp)
	}
}
