//go:build windows

package firewall

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wfpFirewall implements Firewall via Windows Filtering Platform.
type wfpFirewall struct {
	log  Logger
	opts Options

	mu                  sync.Mutex
	expectedFilterCount int
	alteredCh           chan struct{}
	watchdogCancel      context.CancelFunc
}

// New creates a WFP-backed Firewall on Windows.
// The logger is wrapped with Windows Event Log output (source "LeVoile").
func New(log Logger, opts Options) Firewall {
	return &wfpFirewall{
		log:       newEventLogger(log),
		opts:      opts,
		alteredCh: make(chan struct{}, 1),
	}
}

// checkPrerequisites verifies elevation and WFP availability before Activate.
// IsElevated covers both LocalSystem (service) and Administrator (manual run).
// The service always runs as LocalSystem via kardianos/service; admin is
// accepted for manual testing but not the expected production path.
func checkPrerequisites() error {
	if err := modfwpuclnt.Load(); err != nil {
		return ErrWFPUnavailable
	}
	token := windows.GetCurrentProcessToken()
	if !token.IsElevated() {
		return ErrNotElevated
	}
	return nil
}

func (f *wfpFirewall) Activate(ctx context.Context, params ActivateParams) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("firewall: activate cancelled: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Pre-checks: elevation + fwpuclnt.dll availability.
	if err := checkPrerequisites(); err != nil {
		f.errorf("firewall: prerequisite check failed: %v", err)
		return err
	}

	// Stop previous watchdog if re-activating.
	if f.watchdogCancel != nil {
		f.watchdogCancel()
		f.watchdogCancel = nil
	}

	start := time.Now()

	// Open WFP engine.
	engine, err := openEngine()
	if err != nil {
		return err
	}
	defer engine.close()

	// Start transaction.
	if err := engine.beginTransaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			engine.abortTransaction()
		}
	}()

	// Register provider.
	providerName := utf16Ptr("Le Voile VPN")
	providerDesc := utf16Ptr("Kill-switch firewall provider for Le Voile")
	provider := fwpmProvider0{
		providerKey: leVoileProviderKey,
		displayData: fwpmDisplayData0{name: providerName, description: providerDesc},
	}
	if err := engine.addProvider(&provider); err != nil {
		return err
	}

	// Register sublayer with maximum weight (0xFFFF) to precede Windows Firewall.
	sublayerName := utf16Ptr("Le Voile Kill-Switch")
	sublayerDesc := utf16Ptr("Deny-all sublayer with exceptions for TUN and relay")
	provKeyForSublayer := leVoileProviderKey
	sublayer := fwpmSublayer0{
		subLayerKey: leVoileSublayerKey,
		displayData: fwpmDisplayData0{name: sublayerName, description: sublayerDesc},
		providerKey: &provKeyForSublayer,
		weight:      0xFFFF,
	}
	if err := engine.addSublayer(&sublayer); err != nil {
		return err
	}

	// Loopback flag — used in multiple filters below.
	loopbackFlag := uint32(fwpConditionFlagIsLoopback)

	filterCount := 0
	addFilter := func(name string, layer windows.GUID, action uint32, weight uint8, conditions []fwpmFilterCondition0) error {
		provKeyForFilter := leVoileProviderKey
		weightVal := fwpValue0{dataType: fwpUint8, value: uintptr(weight)}
		filter := fwpmFilter0{
			displayData: fwpmDisplayData0{name: utf16Ptr(name)},
			providerKey: &provKeyForFilter,
			layerKey:    layer,
			subLayerKey: leVoileSublayerKey,
			weight:      weightVal,
			action:      fwpmAction0{actionType: action},
		}
		if len(conditions) > 0 {
			filter.numFilterConditions = uint32(len(conditions))
			filter.filterCondition = &conditions[0]
		}
		if _, err := engine.addFilter(&filter); err != nil {
			return fmt.Errorf("firewall: add filter %q: %w", name, err)
		}
		filterCount++
		return nil
	}

	// --- BLOCK filters (weight 10) — common to all modes ---

	// 1. BLOCK all outbound IPv4
	if err := addFilter("LeVoile Block Outbound V4",
		fwpmLayerALEAuthConnectV4, fwpActionBlock, 10, nil); err != nil {
		return err
	}

	// 2. BLOCK all inbound IPv4 (non-loopback)
	if err := addFilter("LeVoile Block Inbound V4",
		fwpmLayerALEAuthRecvAcceptV4, fwpActionBlock, 10, nil); err != nil {
		return err
	}

	// 3. BLOCK all outbound IPv6 (if !AllowIPv6Leak)
	if !f.opts.AllowIPv6Leak {
		if err := addFilter("LeVoile Block Outbound V6",
			fwpmLayerALEAuthConnectV6, fwpActionBlock, 10, nil); err != nil {
			return err
		}

		// 3b. ALLOW IPv6 loopback outbound (IPC may use [::1])
		if err := addFilter("LeVoile Allow Loopback Out V6",
			fwpmLayerALEAuthConnectV6, fwpActionPermit, 15,
			[]fwpmFilterCondition0{{
				fieldKey:       fwpmConditionFlags,
				matchType:      fwpMatchFlagsAllSet,
				conditionValue: fwpValue0{dataType: fwpUint32, value: uintptr(loopbackFlag)},
			}}); err != nil {
			return err
		}
	}

	// 4. BLOCK all inbound IPv6
	if err := addFilter("LeVoile Block Inbound V6",
		fwpmLayerALEAuthRecvAcceptV6, fwpActionBlock, 10, nil); err != nil {
		return err
	}

	// 4b. ALLOW IPv6 loopback inbound
	if err := addFilter("LeVoile Allow Loopback In V6",
		fwpmLayerALEAuthRecvAcceptV6, fwpActionPermit, 15,
		[]fwpmFilterCondition0{{
			fieldKey:       fwpmConditionFlags,
			matchType:      fwpMatchFlagsAllSet,
			conditionValue: fwpValue0{dataType: fwpUint32, value: uintptr(loopbackFlag)},
		}}); err != nil {
		return err
	}

	// --- ALLOW filters (weight 15) — mode-specific ---

	// Variables that need KeepAlive at the end (only allocated for ModeFull).
	var luidPtr *uint64
	var relayAddrMask *fwpV4AddrAndMask

	switch params.Mode {
	case ModeCaptive:
		// Captive mode: allow traffic needed for Wi-Fi portal authentication.
		// Portal login pages are often hosted on a different IP than the
		// gateway (e.g. hotel cloud portals), so we must allow DNS, HTTP,
		// and HTTPS to any destination — not just the gateway.
		gwIP := params.LanGateway.To4()
		if gwIP == nil {
			return fmt.Errorf("firewall: LAN gateway must be IPv4, got %s", params.LanGateway)
		}
		gwAddr := binary.BigEndian.Uint32(gwIP)
		gwAddrMask := &fwpV4AddrAndMask{addr: gwAddr, mask: 0xFFFFFFFF}

		// ALLOW gateway outbound (any port — covers local portal pages on AP).
		if err := addFilter("LeVoile Allow Gateway Out",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{{
				fieldKey:       fwpmConditionIPRemoteAddress,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpV4AddrMask, value: uintptr(unsafe.Pointer(gwAddrMask))},
			}}); err != nil {
			return err
		}

		// ALLOW gateway inbound (for portal responses).
		gwAddrMaskIn := &fwpV4AddrAndMask{addr: gwAddr, mask: 0xFFFFFFFF}
		if err := addFilter("LeVoile Allow Gateway In",
			fwpmLayerALEAuthRecvAcceptV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{{
				fieldKey:       fwpmConditionIPRemoteAddress,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpV4AddrMask, value: uintptr(unsafe.Pointer(gwAddrMaskIn))},
			}}); err != nil {
			return err
		}

		// ALLOW DNS outbound (UDP 53) — portal DNS may not be the gateway.
		if err := addFilter("LeVoile Captive Allow DNS Out",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{
				{
					fieldKey:       fwpmConditionIPProtocol,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoUDP},
				},
				{
					fieldKey:       fwpmConditionIPRemotePort,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint16, value: 53},
				},
			}); err != nil {
			return err
		}

		// ALLOW HTTP outbound (TCP 80) — portal login pages.
		if err := addFilter("LeVoile Captive Allow HTTP Out",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{
				{
					fieldKey:       fwpmConditionIPProtocol,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoTCP},
				},
				{
					fieldKey:       fwpmConditionIPRemotePort,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint16, value: 80},
				},
			}); err != nil {
			return err
		}

		// ALLOW HTTPS outbound (TCP 443) — portal login pages over TLS.
		if err := addFilter("LeVoile Captive Allow HTTPS Out",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{
				{
					fieldKey:       fwpmConditionIPProtocol,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoTCP},
				},
				{
					fieldKey:       fwpmConditionIPRemotePort,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint16, value: 443},
				},
			}); err != nil {
			return err
		}

		runtime.KeepAlive(gwAddrMask)
		runtime.KeepAlive(gwAddrMaskIn)

	default: // ModeFull
		// Full mode: allow TUN + relay IP:443/UDP.
		relayIP := params.RelayIP
		tunName := params.TunName

		luid, lerr := interfaceLUID(tunName)
		if lerr != nil {
			f.errorf("firewall: LUID resolution failed: %v", lerr)
			return fmt.Errorf("firewall: resolve LUID for %s: %w", tunName, lerr)
		}
		f.debugf("resolved %s LUID=%d", tunName, luid)

		ip4 := relayIP.To4()
		if ip4 == nil {
			return fmt.Errorf("firewall: relay IP must be IPv4, got %s", relayIP)
		}
		relayAddr := binary.BigEndian.Uint32(ip4)

		luidPtr = new(uint64)
		*luidPtr = luid

		// 5. ALLOW TUN interface outbound
		if err := addFilter("LeVoile Allow TUN Outbound",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{{
				fieldKey:  fwpmConditionIPLocalInterface,
				matchType: fwpMatchEqual,
				conditionValue: fwpValue0{
					dataType: fwpUint64,
					value:    uintptr(unsafe.Pointer(luidPtr)),
				},
			}}); err != nil {
			return err
		}

		// 6. ALLOW TUN interface inbound
		if err := addFilter("LeVoile Allow TUN Inbound",
			fwpmLayerALEAuthRecvAcceptV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{{
				fieldKey:  fwpmConditionIPLocalInterface,
				matchType: fwpMatchEqual,
				conditionValue: fwpValue0{
					dataType: fwpUint64,
					value:    uintptr(unsafe.Pointer(luidPtr)),
				},
			}}); err != nil {
			return err
		}

		// 7. ALLOW relay IP:443/UDP outbound
		relayAddrMask = &fwpV4AddrAndMask{addr: relayAddr, mask: 0xFFFFFFFF}
		if err := addFilter("LeVoile Allow Relay QUIC",
			fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
			[]fwpmFilterCondition0{
				{
					fieldKey:       fwpmConditionIPRemoteAddress,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpV4AddrMask, value: uintptr(unsafe.Pointer(relayAddrMask))},
				},
				{
					fieldKey:       fwpmConditionIPProtocol,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoUDP},
				},
				{
					fieldKey:       fwpmConditionIPRemotePort,
					matchType:      fwpMatchEqual,
					conditionValue: fwpValue0{dataType: fwpUint16, value: 443},
				},
			}); err != nil {
			return err
		}
	}

	// --- Loopback + DHCP (common to all modes) ---

	// 8. ALLOW loopback outbound (IPC local)
	if err := addFilter("LeVoile Allow Loopback Out",
		fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
		[]fwpmFilterCondition0{{
			fieldKey:       fwpmConditionFlags,
			matchType:      fwpMatchFlagsAllSet,
			conditionValue: fwpValue0{dataType: fwpUint32, value: uintptr(loopbackFlag)},
		}}); err != nil {
		return err
	}

	// 9. ALLOW loopback inbound
	if err := addFilter("LeVoile Allow Loopback In",
		fwpmLayerALEAuthRecvAcceptV4, fwpActionPermit, 15,
		[]fwpmFilterCondition0{{
			fieldKey:       fwpmConditionFlags,
			matchType:      fwpMatchFlagsAllSet,
			conditionValue: fwpValue0{dataType: fwpUint32, value: uintptr(loopbackFlag)},
		}}); err != nil {
		return err
	}

	// 10. ALLOW DHCP outbound (UDP 67) — lease renewal
	if err := addFilter("LeVoile Allow DHCP Out",
		fwpmLayerALEAuthConnectV4, fwpActionPermit, 15,
		[]fwpmFilterCondition0{
			{
				fieldKey:       fwpmConditionIPProtocol,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoUDP},
			},
			{
				fieldKey:       fwpmConditionIPRemotePort,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpUint16, value: 67},
			},
		}); err != nil {
		return err
	}

	// 11. ALLOW DHCP inbound (UDP 68)
	if err := addFilter("LeVoile Allow DHCP In",
		fwpmLayerALEAuthRecvAcceptV4, fwpActionPermit, 15,
		[]fwpmFilterCondition0{
			{
				fieldKey:       fwpmConditionIPProtocol,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoUDP},
			},
			{
				fieldKey:       fwpmConditionIPLocalPort,
				matchType:      fwpMatchEqual,
				conditionValue: fwpValue0{dataType: fwpUint16, value: 68},
			},
		}); err != nil {
		return err
	}

	// Commit transaction.
	if err := engine.commitTransaction(); err != nil {
		return err
	}
	committed = true

	// Prevent GC from collecting heap objects referenced via uintptr in
	// condition values before the transaction commit copies the data.
	runtime.KeepAlive(luidPtr)
	runtime.KeepAlive(relayAddrMask)

	f.expectedFilterCount = filterCount

	dur := time.Since(start)
	// NFR22a: no user data (relayIP, tunName) in Event Log messages.
	f.infof("WFP activated mode=%s filters=%d duration_ms=%d", params.Mode, filterCount, dur.Milliseconds())
	if dur > 100*time.Millisecond {
		f.warnf("WFP activation exceeded 100ms NFR15 target: %v", dur)
	}

	// Start watchdog goroutine.
	wdCtx, wdCancel := context.WithCancel(context.Background())
	f.watchdogCancel = wdCancel
	go f.watchdog(wdCtx)

	return nil
}

func (f *wfpFirewall) Deactivate(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Stop watchdog.
	if f.watchdogCancel != nil {
		f.watchdogCancel()
		f.watchdogCancel = nil
	}

	engine, err := openEngine()
	if err != nil {
		return err
	}
	defer engine.close()

	if err := engine.beginTransaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			engine.abortTransaction()
		}
	}()

	// Delete all filters by provider.
	ids, err := engine.enumFiltersByProvider(&leVoileProviderKey)
	if err != nil {
		// If enum fails, try to delete sublayer/provider anyway.
		f.warnf("enum filters failed: %v", err)
	} else {
		for _, id := range ids {
			if delErr := engine.deleteFilter(id); delErr != nil {
				f.warnf("delete filter %d: %v", id, delErr)
			}
		}
	}

	// Delete sublayer and provider (idempotent).
	if err := engine.deleteSublayer(&leVoileSublayerKey); err != nil {
		f.warnf("delete sublayer: %v", err)
	}
	if err := engine.deleteProvider(&leVoileProviderKey); err != nil {
		f.warnf("delete provider: %v", err)
	}

	if err := engine.commitTransaction(); err != nil {
		return err
	}
	committed = true
	f.expectedFilterCount = 0

	f.infof("WFP deactivated")
	return nil
}

func (f *wfpFirewall) IsActive(_ context.Context) (bool, error) {
	engine, err := openEngine()
	if err != nil {
		return false, err
	}
	defer engine.close()

	ids, err := engine.enumFiltersByProvider(&leVoileProviderKey)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func (f *wfpFirewall) CleanupOrphans(_ context.Context) (int, error) {
	engine, err := openEngine()
	if err != nil {
		return 0, err
	}
	defer engine.close()

	ids, err := engine.enumFiltersByProvider(&leVoileProviderKey)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	if err := engine.beginTransaction(); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			engine.abortTransaction()
		}
	}()

	for _, id := range ids {
		if delErr := engine.deleteFilter(id); delErr != nil {
			f.warnf("cleanup orphan filter %d: %v", id, delErr)
		}
	}
	_ = engine.deleteSublayer(&leVoileSublayerKey)
	_ = engine.deleteProvider(&leVoileProviderKey)

	if err := engine.commitTransaction(); err != nil {
		return 0, err
	}
	committed = true

	f.infof("WFP orphans cleaned: %d", len(ids))
	return len(ids), nil
}

func (f *wfpFirewall) AlteredCh() <-chan struct{} {
	return f.alteredCh
}

// Logging helpers — no-op if logger is nil.

func (f *wfpFirewall) infof(format string, args ...any) {
	if f.log != nil {
		f.log.Infof(format, args...)
	}
}

func (f *wfpFirewall) warnf(format string, args ...any) {
	if f.log != nil {
		f.log.Warnf(format, args...)
	}
}

func (f *wfpFirewall) errorf(format string, args ...any) {
	if f.log != nil {
		f.log.Errorf(format, args...)
	}
}

func (f *wfpFirewall) debugf(format string, args ...any) {
	if f.log != nil {
		f.log.Debugf(format, args...)
	}
}
