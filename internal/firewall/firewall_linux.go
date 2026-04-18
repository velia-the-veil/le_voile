//go:build linux

package firewall

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// nftFirewall implements Firewall via nftables shellout on Linux.
type nftFirewall struct {
	mu         sync.Mutex
	log        Logger
	opts       Options
	run        commandRunner // for simple commands (list, delete)
	stdinRun   stdinRunner   // for nft -f - (stdin pipe)
	lastParams *ActivateParams // stored for SetIPv6Policy re-apply
}

// New creates a Linux Firewall backed by nftables.
// Logger may be nil (silent operation).
func New(log Logger, opts Options) Firewall {
	return &nftFirewall{
		log:      log,
		opts:     opts,
		run:      defaultRunner,
		stdinRun: defaultStdinRunner,
	}
}

func (f *nftFirewall) Activate(ctx context.Context, params ActivateParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activateLocked(ctx, params)
}

// activateLocked performs the actual activation. Caller must hold f.mu.
func (f *nftFirewall) activateLocked(ctx context.Context, params ActivateParams) error {
	// Phase 1: detect nft binary + kernel module
	if err := f.detectNft(ctx); err != nil {
		f.errorf("firewall activation failed: %v", err)
		return err
	}

	// Phase 2: check for orphan ruleset (log WARN if present)
	if active, _ := f.isActiveLocked(ctx); active {
		f.warnf("orphan nftables ruleset detected, replacing")
	}

	// Phase 3: render + apply + verify (timed as a whole per AC1 NFR15)
	start := time.Now()

	var script string
	var err error
	switch params.Mode {
	case ModeCaptive:
		script, err = renderCaptiveRuleset(params.LanGateway)
	default: // ModeFull
		script, err = renderRuleset(params.RelayIP, params.TunName, f.opts.AllowIPv6Leak)
	}
	if err != nil {
		f.errorf("ruleset render failed: %v", err)
		return err
	}
	f.debugf("ruleset script (%d bytes): %.200s", len(script), script)

	// Phase 4: atomic apply via nft -f -
	if err := f.applyRuleset(ctx, script); err != nil {
		f.errorf("nft apply failed: %v", err)
		return err
	}

	// Phase 5: verify post-apply
	if active, err := f.isActiveLocked(ctx); err != nil {
		f.errorf("post-apply verification failed: %v", err)
		return fmt.Errorf("firewall: post-apply check failed: %w", err)
	} else if !active {
		return fmt.Errorf("firewall: ruleset not active after apply")
	}

	f.lastParams = &params

	dur := time.Since(start)
	f.infof("firewall activated mode=%s duration_ms=%d", params.Mode, dur.Milliseconds())
	return nil
}

func (f *nftFirewall) Deactivate(ctx context.Context) error {
	out, err := f.run(ctx, "nft", "delete", "table", "inet", "levoile")
	if err != nil {
		// "No such file or directory" means table already gone → idempotent
		if strings.Contains(string(out), "No such file or directory") {
			return nil
		}
		return fmt.Errorf("firewall: deactivate: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	f.infof("firewall deactivated")
	return nil
}

func (f *nftFirewall) IsActive(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isActiveLocked(ctx)
}

func (f *nftFirewall) isActiveLocked(ctx context.Context) (bool, error) {
	out, err := f.run(ctx, "nft", "list", "table", "inet", "levoile")
	if err != nil {
		if strings.Contains(string(out), "No such file or directory") {
			return false, nil
		}
		return false, fmt.Errorf("firewall: isactive check: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// SetIPv6Policy updates AllowIPv6Leak and re-applies the ruleset atomically.
// The firewall must have been activated at least once (lastParams stored).
func (f *nftFirewall) SetIPv6Policy(ctx context.Context, allow bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastParams == nil {
		return fmt.Errorf("firewall: SetIPv6Policy called before Activate")
	}
	f.opts.AllowIPv6Leak = allow
	return f.activateLocked(ctx, *f.lastParams)
}

// CleanupOrphans is a no-op on Linux: nftables Activate replaces atomically.
func (f *nftFirewall) CleanupOrphans(_ context.Context) (int, error) { return 0, nil }

// AlteredCh returns nil on Linux (no watchdog implemented yet).
func (f *nftFirewall) AlteredCh() <-chan struct{} { return nil }

// Logging helpers — no-op if logger is nil.

func (f *nftFirewall) infof(format string, args ...any) {
	if f.log != nil {
		f.log.Infof(format, args...)
	}
}

func (f *nftFirewall) warnf(format string, args ...any) {
	if f.log != nil {
		f.log.Warnf(format, args...)
	}
}

func (f *nftFirewall) errorf(format string, args ...any) {
	if f.log != nil {
		f.log.Errorf(format, args...)
	}
}

func (f *nftFirewall) debugf(format string, args ...any) {
	if f.log != nil {
		f.log.Debugf(format, args...)
	}
}
