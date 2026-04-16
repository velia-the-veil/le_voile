//go:build windows

package dns

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestFlush_Windows(t *testing.T) {
	origLog := flushLogWriter
	flushLogWriter = io.Discard
	defer func() { flushLogWriter = origLog }()

	tests := []struct {
		name      string
		runnerOut []byte
		runnerErr error
		wantErr   bool
	}{
		{
			name:      "success",
			runnerOut: []byte("Successfully flushed the DNS Resolver Cache.\r\n"),
			runnerErr: nil,
			wantErr:   false,
		},
		{
			name:      "ipconfig fails",
			runnerOut: []byte("error output"),
			runnerErr: errors.New("exit status 1"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calledName string
			var calledArgs []string

			orig := flushRunner
			flushRunner = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calledName = name
				calledArgs = args
				return tt.runnerOut, tt.runnerErr
			}
			defer func() { flushRunner = orig }()

			err := Flush(context.Background())

			if calledName != "ipconfig" {
				t.Errorf("expected command 'ipconfig', got %q", calledName)
			}
			if len(calledArgs) != 1 || calledArgs[0] != "/flushdns" {
				t.Errorf("expected args [/flushdns], got %v", calledArgs)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("Flush() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFlush_Windows_ContextCancelled(t *testing.T) {
	origLog := flushLogWriter
	flushLogWriter = io.Discard
	defer func() { flushLogWriter = origLog }()

	orig := flushRunner
	flushRunner = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, ctx.Err()
	}
	defer func() { flushRunner = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Flush(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
