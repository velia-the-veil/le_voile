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
	handler := NewHealthHandler(limiter, nil, time.Now())
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
	handler := NewHealthHandler(limiter, nil, startTime)
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
	handler := NewHealthHandler(limiter, nil, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// Check for actual IP addresses or user-identifying data (not generic field names
	// like "ip_limit" which are operational counters, not sensitive data).
	bodyStr := string(body)
	for _, forbidden := range []string{"client_ip", "dns_query", "header", "remote_addr", "cf-connecting"} {
		if strings.Contains(strings.ToLower(bodyStr), forbidden) {
			t.Errorf("response contains potentially sensitive field %q: %s", forbidden, bodyStr)
		}
	}
}

func TestHealthHandler_ContentType(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, nil, time.Now())
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
	handler := NewHealthHandler(limiter, nil, time.Now())

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

func TestHealthHandler_NATEntries(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, nil, time.Now())
	handler.SetNATStatsProvider(func() NATStats {
		return NATStats{Entries: 42, PortsUsed: 42}
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hr.NATEntries != 42 {
		t.Errorf("nat_entries=%d, want 42", hr.NATEntries)
	}
}

func TestHealthHandler_NATEntriesOmittedWhenNoProvider(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, nil, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	if strings.Contains(string(body), "nat_entries") {
		t.Errorf("nat_entries should be omitted when no provider: %s", body)
	}
}

func TestHealthHandler_ExposesRateLimitCounters(t *testing.T) {
	// Reset global counters and ensure cleanup even on failure.
	RejectedIPLimitTotal.Store(0)
	RejectedDailyQuotaTotal.Store(0)
	ThrottledHourlyQuotaTotal.Store(0)
	t.Cleanup(func() {
		RejectedIPLimitTotal.Store(0)
		RejectedDailyQuotaTotal.Store(0)
		ThrottledHourlyQuotaTotal.Store(0)
	})

	// Simulate some events.
	RejectedIPLimitTotal.Add(3)
	RejectedDailyQuotaTotal.Add(1)
	ThrottledHourlyQuotaTotal.Add(7)

	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, nil, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if hr.RejectedIPLimitTotal != 3 {
		t.Errorf("rejected_ip_limit_total = %d, want 3", hr.RejectedIPLimitTotal)
	}
	if hr.RejectedDailyQuotaTotal != 1 {
		t.Errorf("rejected_daily_quota_total = %d, want 1", hr.RejectedDailyQuotaTotal)
	}
	if hr.ThrottledHourlyQuotaTotal != 7 {
		t.Errorf("throttled_hourly_quota_total = %d, want 7", hr.ThrottledHourlyQuotaTotal)
	}

	// Cleanup handled by t.Cleanup above.
}

func TestHealthHandler_ExposesTunnelsField(t *testing.T) {
	legacy := NewLimiter(MaxConnections)
	tun := NewLimiter(MaxTunnels)
	for i := 0; i < 3; i++ {
		tun.Acquire()
	}
	defer func() {
		for i := 0; i < 3; i++ {
			tun.Release()
		}
	}()

	handler := NewHealthHandler(legacy, tun, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hr.Tunnels != 3 {
		t.Errorf("tunnels = %d, want 3", hr.Tunnels)
	}
	// Verify JSON tag exists in raw output
	if !strings.Contains(string(body), `"tunnels":3`) {
		t.Errorf("expected raw JSON to contain '\"tunnels\":3', got %s", body)
	}
}

func TestHealthHandler_TunnelsZeroWhenNilLimiter(t *testing.T) {
	limiter := NewLimiter(MaxConnections)
	handler := NewHealthHandler(limiter, nil, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hr.Tunnels != 0 {
		t.Errorf("tunnels = %d, want 0 when tunnelLimiter is nil", hr.Tunnels)
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
