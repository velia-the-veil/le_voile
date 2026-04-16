//go:build windows

package firewall

import (
	"context"
	"time"
)

const watchdogPollInterval = 3 * time.Second

// watchdog polls WFP filter count every 3s and sends on alteredCh if filters
// were removed by a third-party (AV/firewall). Stopped via context cancellation
// at Deactivate.
func (f *wfpFirewall) watchdog(ctx context.Context) {
	ticker := time.NewTicker(watchdogPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.mu.Lock()
			expected := f.expectedFilterCount
			f.mu.Unlock()

			if expected == 0 {
				continue // firewall not active
			}

			engine, err := openEngine()
			if err != nil {
				f.warnf("watchdog: open engine: %v", err)
				continue
			}
			ids, err := engine.enumFiltersByProvider(&leVoileProviderKey)
			engine.close()
			if err != nil {
				f.warnf("watchdog: enum filters: %v", err)
				continue
			}

			if len(ids) < expected {
				f.warnf("WFP altered by third party: expected %d filters, found %d", expected, len(ids))
				// Non-blocking send — service observes via AlteredCh().
				select {
				case f.alteredCh <- struct{}{}:
				default:
				}
			}
		}
	}
}
