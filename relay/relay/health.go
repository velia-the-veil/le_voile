package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

// Rate limiting counters — package-level atomics, aggregate only (never per-IP).
var (
	RejectedIPLimitTotal     atomic.Int64
	RejectedDailyQuotaTotal  atomic.Int64
	ThrottledHourlyQuotaTotal atomic.Int64
)

// NATStatsProvider returns NAT table statistics for the health endpoint.
type NATStatsProvider func() NATStats

// HealthResponse contains relay health metrics.
type HealthResponse struct {
	Status                    string  `json:"status"`
	Connections               int64   `json:"connections"`
	Tunnels                   int64   `json:"tunnels"`
	NATEntries                int64   `json:"nat_entries,omitempty"`
	Uptime                    string  `json:"uptime"`
	RAMMB                     float64 `json:"ram_mb"`
	CPUPct                    float64 `json:"cpu_pct"`
	RejectedIPLimitTotal      int64   `json:"rejected_ip_limit_total"`
	RejectedDailyQuotaTotal   int64   `json:"rejected_daily_quota_total"`
	ThrottledHourlyQuotaTotal int64   `json:"throttled_hourly_quota_total"`
}

// HealthHandler serves enriched health check responses with metrics.
type HealthHandler struct {
	limiter       *Limiter
	tunnelLimiter *Limiter
	startTime     time.Time
	natStatsFunc  NATStatsProvider
}

// NewHealthHandler creates a HealthHandler with metrics dependencies.
// tunnelLimiter may be nil (tunnels field will report 0).
func NewHealthHandler(limiter *Limiter, tunnelLimiter *Limiter, startTime time.Time) *HealthHandler {
	return &HealthHandler{
		limiter:       limiter,
		tunnelLimiter: tunnelLimiter,
		startTime:     startTime,
	}
}

// SetNATStatsProvider sets the callback for NAT table stats.
func (h *HealthHandler) SetNATStatsProvider(fn NATStatsProvider) {
	h.natStatsFunc = fn
}

// ServeHTTP responds with JSON health metrics. Only GET is allowed.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	var tunnels int64
	if h.tunnelLimiter != nil {
		tunnels = h.tunnelLimiter.Current()
	}

	resp := HealthResponse{
		Status:                    "ok",
		Connections:               h.limiter.Current(),
		Tunnels:                   tunnels,
		Uptime:                    formatUptime(time.Since(h.startTime)),
		RAMMB:                     float64(memStats.Sys) / 1024 / 1024,
		CPUPct:                    0.0, // TODO: implement CPU sampling
		RejectedIPLimitTotal:      RejectedIPLimitTotal.Load(),
		RejectedDailyQuotaTotal:   RejectedDailyQuotaTotal.Load(),
		ThrottledHourlyQuotaTotal: ThrottledHourlyQuotaTotal.Load(),
	}
	if h.natStatsFunc != nil {
		resp.NATEntries = h.natStatsFunc().Entries
	}

	data, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// formatUptime formats a duration into a human-readable string.
func formatUptime(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
