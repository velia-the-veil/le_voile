package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServer_InvalidTLSCertKeyPaths(t *testing.T) {
	tests := []struct {
		name     string
		certFile string
		keyFile  string
	}{
		{
			name:     "nonexistent_cert",
			certFile: "/nonexistent/path/cert.pem",
			keyFile:  "/nonexistent/path/key.pem",
		},
		{
			name:     "empty_paths",
			certFile: "",
			keyFile:  "",
		},
		{
			name:     "cert_exists_key_missing",
			certFile: "server.go", // exists but not a valid cert
			keyFile:  "/nonexistent/key.pem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer("127.0.0.1:0", tt.certFile, tt.keyFile)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := srv.ListenAndServe(ctx)
			if err == nil {
				t.Fatal("expected error for invalid TLS cert/key paths, got nil")
			}

			var srvErr *ServerError
			if !errors.As(err, &srvErr) {
				t.Fatalf("expected *ServerError, got %T: %v", err, err)
			}

			if srvErr.Op != "load-tls" {
				t.Errorf("expected Op = %q, got %q", "load-tls", srvErr.Op)
			}
		})
	}
}
