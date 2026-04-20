//go:build ipc_diagnostic

// Tiny IPC client used to drive the service's named pipe directly during
// diagnostics — bypasses the UI and ctl. Excluded from regular builds via
// the ipc_diagnostic build tag; opt in with:
//
//	go build -tags ipc_diagnostic -o ipc_send.exe ./tools/ipc_send
//
// Usage:
//   ipc_send.exe '{"action":"select_country","value":"de"}'
//   ipc_send.exe '{"action":"get_status"}'
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ipc_send '<json>'")
		os.Exit(2)
	}
	payload := os.Args[1]
	// Validate JSON
	var any map[string]any
	if err := json.Unmarshal([]byte(payload), &any); err != nil {
		fmt.Fprintf(os.Stderr, "invalid json: %v\n", err)
		os.Exit(2)
	}
	t0 := time.Now()
	timeout := 30 * time.Second
	conn, err := winio.DialPipe(`\\.\pipe\levoile`, &timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := fmt.Fprintln(conn, payload); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		fmt.Fprintf(os.Stderr, "no response after %.1fs: %v\n", time.Since(t0).Seconds(), scanner.Err())
		os.Exit(1)
	}
	fmt.Printf("[%.2fs] %s\n", time.Since(t0).Seconds(), scanner.Text())
}
