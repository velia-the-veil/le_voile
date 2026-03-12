package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthHandler_ReturnsOK(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hr.Status != "ok" {
		t.Errorf("expected status ok, got %q", hr.Status)
	}
}

func TestHealthHandler_ReturnsFullMetrics(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	limiter.Acquire()
	defer limiter.Release()

	startTime := time.Now().Add(-25 * time.Hour) // 1d1h ago
	handler := NewHealthHandler(limiter, startTime)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if hr.Status != "ok" {
		t.Errorf("expected status ok, got %q", hr.Status)
	}
	if hr.Connections != 1 {
		t.Errorf("expected connections 1, got %d", hr.Connections)
	}
	if !strings.HasPrefix(hr.Uptime, "1d") {
		t.Errorf("expected uptime starting with 1d, got %q", hr.Uptime)
	}
	if hr.RAMMB <= 0 {
		t.Errorf("expected ram_mb > 0, got %f", hr.RAMMB)
	}
	if hr.CPUPct != 0.0 {
		t.Errorf("expected cpu_pct 0.0 (MVP), got %f", hr.CPUPct)
	}
}

func TestHealthHandler_NoSensitiveData(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	bodyStr := string(body)
	for _, forbidden := range []string{"ip", "client", "dns", "query", "header", "remote"} {
		if strings.Contains(strings.ToLower(bodyStr), forbidden) {
			t.Errorf("response contains potentially sensitive field %q: %s", forbidden, bodyStr)
		}
	}
}

func TestHealthHandler_ContentType(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	ct := rec.Result().Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected content-type application/json, got %q", ct)
	}
}

func TestHealthHandler_MethodGET(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, time.Now())

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, rec.Code)
		}
	}
}

func TestHealthHandler_FormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"days and hours", 75 * time.Hour, "3d3h"},
		{"hours and minutes", 2*time.Hour + 45*time.Minute, "2h45m"},
		{"minutes and seconds", 30*time.Minute + 12*time.Second, "30m12s"},
		{"zero", 0, "0m0s"},
		{"one day exactly", 24 * time.Hour, "1d0h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUptime(tt.duration)
			if got != tt.expected {
				t.Errorf("formatUptime(%v) = %q, want %q", tt.duration, got, tt.expected)
			}
		})
	}
}
