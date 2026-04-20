//go:build windows

package firewall

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WFP syscall procedures from fwpuclnt.dll and iphlpapi.dll.
var (
	modfwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")
	modiphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procFwpmEngineOpen0              = modfwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0             = modfwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmTransactionBegin0        = modfwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0       = modfwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0        = modfwpuclnt.NewProc("FwpmTransactionAbort0")
	procFwpmProviderAdd0             = modfwpuclnt.NewProc("FwpmProviderAdd0")
	procFwpmProviderDeleteByKey0     = modfwpuclnt.NewProc("FwpmProviderDeleteByKey0")
	procFwpmSubLayerAdd0             = modfwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmSubLayerDeleteByKey0     = modfwpuclnt.NewProc("FwpmSubLayerDeleteByKey0")
	procFwpmFilterAdd0               = modfwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmFilterDeleteById0        = modfwpuclnt.NewProc("FwpmFilterDeleteById0")
	procFwpmFilterCreateEnumHandle0  = modfwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0              = modfwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modfwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0              = modfwpuclnt.NewProc("FwpmFreeMemory0")

	procConvertInterfaceAliasToLuid = modiphlpapi.NewProc("ConvertInterfaceAliasToLuid")
)

// Le Voile stable GUIDs — never randomize; crash-recovery relies on these.
var (
	leVoileProviderKey = windows.GUID{
		Data1: 0x4e7c2b4f,
		Data2: 0x8a3d,
		Data3: 0x4f1e,
		Data4: [8]byte{0x9b, 0x5c, 0x6d, 0x8a, 0x2f, 0x1e, 0x3b, 0x5a},
	}
	leVoileSublayerKey = windows.GUID{
		Data1: 0x7b3d5e1a,
		Data2: 0xc8f2,
		Data3: 0x4a6d,
		Data4: [8]byte{0xbe, 0x91, 0x3f, 0x5c, 0x8a, 0x2d, 0x1e, 0x4b},
	}
)

// WFP layer GUIDs (from MSDN).
var (
	fwpmLayerALEAuthConnectV4 = windows.GUID{
		Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33,
		Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82},
	}
	fwpmLayerALEAuthConnectV6 = windows.GUID{
		Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc,
		Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4},
	}
	fwpmLayerALEAuthRecvAcceptV4 = windows.GUID{
		Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273,
		Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50},
	}
	// FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6 — GUID copié depuis le header
	// Windows SDK fwpmu.h. ATTENTION : la page MSDN "management-filtering-
	// layer-identifiers" liste une valeur différente (a3b42c97-9f04-4765-…)
	// qui est fausse : elle ne matche ni l'enum runtime (FwpmLayerEnum0)
	// ni le header officiel. L'utiliser fait échouer FwpmFilterAdd0 avec
	// FWP_E_LAYER_NOT_FOUND (0x80320004), ce qui abort la transaction
	// Activate et laisse WFP à zéro filtre → kill-switch inactif. Vérifié
	// par énumération runtime sur Windows 11 26200 (2026-04-19).
	fwpmLayerALEAuthRecvAcceptV6 = windows.GUID{
		Data1: 0xa3b42c97, Data2: 0x9f04, Data3: 0x4672,
		Data4: [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f},
	}
)

// WFP condition field GUIDs (from MSDN).
var (
	fwpmConditionIPLocalInterface = windows.GUID{
		Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969,
		Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4},
	}
	fwpmConditionIPRemoteAddress = windows.GUID{
		Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8,
		Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45},
	}
	fwpmConditionIPProtocol = windows.GUID{
		Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a,
		Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7},
	}
	// IP_REMOTE_PORT / IP_LOCAL_PORT GUIDs copied from Windows SDK fwpmu.h.
	// The values listed on some MSDN pages differ from the header/runtime and
	// will cause FwpmFilterAdd0 to fail with FWP_E_CONDITION_NOT_FOUND.
	fwpmConditionIPRemotePort = windows.GUID{
		Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a,
		Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b},
	}
	fwpmConditionIPLocalPort = windows.GUID{
		Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f,
		Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b},
	}
	fwpmConditionFlags = windows.GUID{
		Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c,
		Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c},
	}
)

// WFP action types.
const (
	fwpActionBlock  = 0x00001001 // FWP_ACTION_FLAG_TERMINATING | block
	fwpActionPermit = 0x00001002 // FWP_ACTION_FLAG_TERMINATING | permit
)

// FWP data types (enum FWP_DATA_TYPE from Windows SDK fwptypes.h). The
// address-mask types are NOT contiguous with the scalar types: the enum
// defines FWP_SINGLE_DATA_TYPE_MAX = 0xff as a marker, after which
// FWP_V4_ADDR_MASK = 0x100 and FWP_V6_ADDR_MASK = 0x101. Using 19/20 like
// a naive continuation of the scalar block makes FwpmFilterAdd0 reject
// the filter with FWP_E_TYPE_MISMATCH (0x8032001d).
const (
	fwpEmpty      = 0
	fwpUint8      = 1
	fwpUint16     = 2
	fwpUint32     = 3
	fwpUint64     = 4
	fwpV4AddrMask = 0x100
	fwpV6AddrMask = 0x101
)

// FWP match types.
const (
	fwpMatchEqual       = 0
	fwpMatchFlagsAllSet = 6
)

// FWP condition flags.
const (
	fwpConditionFlagIsLoopback = 0x00000001
)

// IP protocol numbers.
const (
	ipprotoTCP = 6
	ipprotoUDP = 17
)

// WFP struct layouts for amd64 Windows.
// Field offsets verified against Windows SDK headers.

type fwpmDisplayData0 struct {
	name        *uint16
	description *uint16
}

type fwpByteBlob struct {
	size uint32
	_    [4]byte
	data *byte
}

type fwpmProvider0 struct {
	providerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	_            [4]byte
	providerData fwpByteBlob
	serviceName  *uint16
}

type fwpmSublayer0 struct {
	subLayerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	_            [4]byte
	providerKey  *windows.GUID
	providerData fwpByteBlob
	weight       uint16
	_2           [6]byte
}

type fwpValue0 struct {
	dataType uint32
	_        [4]byte
	value    uintptr
}

type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	_              [4]byte
	conditionValue fwpValue0
}

type fwpmAction0 struct {
	actionType uint32
	filterType windows.GUID
}

type fwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         fwpmDisplayData0
	flags               uint32
	_pad1               [4]byte
	providerKey         *windows.GUID
	providerData        fwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              fwpValue0
	numFilterConditions uint32
	_pad2               [4]byte
	filterCondition     *fwpmFilterCondition0
	action              fwpmAction0
	_pad3               [4]byte
	contextUnion        [16]byte
	reserved            *windows.GUID
	filterId            uint64
	effectiveWeight     fwpValue0
}

type fwpmFilterEnumTemplate0 struct {
	providerKey            *windows.GUID
	layerKey               windows.GUID
	enumType               uint32
	flags                  uint32
	providerContextTemplate uintptr
	numFilterConditions    uint32
	_pad                   [4]byte
	filterCondition        *fwpmFilterCondition0
	actionMask             uint32
	_pad2                  [4]byte
	calloutKey             *windows.GUID
}

type fwpV4AddrAndMask struct {
	addr uint32
	mask uint32
}

// wfpEngine wraps an open WFP engine handle.
type wfpEngine windows.Handle

// openEngine opens the local WFP engine with default (non-dynamic) session.
func openEngine() (wfpEngine, error) {
	var handle windows.Handle
	r, _, _ := procFwpmEngineOpen0.Call(
		0,                                   // serverName (NULL = local)
		0x0000000a,                          // RPC_C_AUTHN_WINNT
		0,                                   // authIdentity (NULL)
		0,                                   // session (NULL = non-dynamic, persistent)
		uintptr(unsafe.Pointer(&handle)),    // engineHandle
	)
	if r != 0 {
		return 0, fmt.Errorf("firewall: FwpmEngineOpen0: 0x%08x", r)
	}
	return wfpEngine(handle), nil
}

func (e wfpEngine) close() {
	if e != 0 {
		procFwpmEngineClose0.Call(uintptr(e))
	}
}

func (e wfpEngine) beginTransaction() error {
	r, _, _ := procFwpmTransactionBegin0.Call(uintptr(e), 0)
	if r != 0 {
		return fmt.Errorf("firewall: FwpmTransactionBegin0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) commitTransaction() error {
	r, _, _ := procFwpmTransactionCommit0.Call(uintptr(e))
	if r != 0 {
		return fmt.Errorf("firewall: FwpmTransactionCommit0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) abortTransaction() {
	procFwpmTransactionAbort0.Call(uintptr(e))
}

func (e wfpEngine) addProvider(p *fwpmProvider0) error {
	r, _, _ := procFwpmProviderAdd0.Call(
		uintptr(e),
		uintptr(unsafe.Pointer(p)),
		0, // sd
	)
	if r != 0 {
		// 0x80320009 = FWP_E_ALREADY_EXISTS — idempotent
		if r == 0x80320009 {
			return nil
		}
		return fmt.Errorf("firewall: FwpmProviderAdd0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) deleteProvider(key *windows.GUID) error {
	r, _, _ := procFwpmProviderDeleteByKey0.Call(
		uintptr(e),
		uintptr(unsafe.Pointer(key)),
	)
	if r != 0 && r != 0x80320008 { // FWP_E_PROVIDER_NOT_FOUND
		return fmt.Errorf("firewall: FwpmProviderDeleteByKey0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) addSublayer(s *fwpmSublayer0) error {
	r, _, _ := procFwpmSubLayerAdd0.Call(
		uintptr(e),
		uintptr(unsafe.Pointer(s)),
		0, // sd
	)
	if r != 0 {
		if r == 0x80320009 { // ALREADY_EXISTS
			return nil
		}
		return fmt.Errorf("firewall: FwpmSubLayerAdd0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) deleteSublayer(key *windows.GUID) error {
	r, _, _ := procFwpmSubLayerDeleteByKey0.Call(
		uintptr(e),
		uintptr(unsafe.Pointer(key)),
	)
	if r != 0 && r != 0x80320008 { // NOT_FOUND
		return fmt.Errorf("firewall: FwpmSubLayerDeleteByKey0: 0x%08x", r)
	}
	return nil
}

func (e wfpEngine) addFilter(f *fwpmFilter0) (uint64, error) {
	var filterID uint64
	r, _, _ := procFwpmFilterAdd0.Call(
		uintptr(e),
		uintptr(unsafe.Pointer(f)),
		0, // sd
		uintptr(unsafe.Pointer(&filterID)),
	)
	if r != 0 {
		return 0, fmt.Errorf("firewall: FwpmFilterAdd0: 0x%08x", r)
	}
	return filterID, nil
}

func (e wfpEngine) deleteFilter(filterID uint64) error {
	r, _, _ := procFwpmFilterDeleteById0.Call(
		uintptr(e),
		uintptr(filterID),
	)
	if r != 0 && r != 0x80320003 { // FWP_E_FILTER_NOT_FOUND
		return fmt.Errorf("firewall: FwpmFilterDeleteById0: 0x%08x", r)
	}
	return nil
}

// enumFiltersByProvider enumerates all filter IDs owned by providerKey.
//
// Implementation note: passing a non-NULL FWPM_FILTER_ENUM_TEMPLATE0 with
// just providerKey set causes FwpmFilterCreateEnumHandle0 to return
// FWP_E_NEVER_MATCH (0x80320033) on Windows 11 — confirmed by repro across
// {FULLY_CONTAINED, OVERLAPPING}, with/without flags, with/without
// actionMask. Adding actionMask=0xFFFFFFFF flips the failure to
// FWP_E_LAYER_NOT_FOUND. The only template that works is NULL, which
// enumerates every filter on the system. We then filter client-side by
// providerKey GUID. Cost: ~800 filters per scan on a typical Windows
// install — negligible in practice.
//
// Symptoms when broken: Activate logs "WFP activated mode=full filters=N",
// the watchdog (every 3s) calls this and gets 0, declares "altered by third
// party", calls Deactivate (which silently leaves orphans because
// enumFiltersByProvider returns 0 → no filters deleted, then sublayer/
// provider delete fail with FWP_E_IN_USE), then re-Activates → infinite
// loop, accumulating orphan filters every cycle.
func (e wfpEngine) enumFiltersByProvider(providerKey *windows.GUID) ([]uint64, error) {
	if providerKey == nil {
		return nil, fmt.Errorf("firewall: enumFiltersByProvider: nil providerKey")
	}

	var enumHandle windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(
		uintptr(e),
		0, // NULL template — see note above
		uintptr(unsafe.Pointer(&enumHandle)),
	)
	if r != 0 {
		return nil, fmt.Errorf("firewall: FwpmFilterCreateEnumHandle0: 0x%08x", r)
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(uintptr(e), uintptr(enumHandle))

	want := *providerKey
	var ids []uint64
	for {
		var entries **fwpmFilter0
		var count uint32
		r, _, _ = procFwpmFilterEnum0.Call(
			uintptr(e),
			uintptr(enumHandle),
			1000, // numEntriesRequested — large enough to drain in one call on most systems
			uintptr(unsafe.Pointer(&entries)),
			uintptr(unsafe.Pointer(&count)),
		)
		if r != 0 {
			return nil, fmt.Errorf("firewall: FwpmFilterEnum0: 0x%08x", r)
		}
		if count == 0 {
			break
		}

		// entries is **fwpmFilter0 — array of pointers allocated by WFP.
		ptrs := unsafe.Slice(entries, count)
		for _, fp := range ptrs {
			if fp.providerKey != nil && *fp.providerKey == want {
				ids = append(ids, fp.filterId)
			}
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
	}
	return ids, nil
}

// interfaceLUID resolves a network interface alias (e.g. "levoile0") to its LUID.
func interfaceLUID(name string) (uint64, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("firewall: UTF16 name: %w", err)
	}
	var luid uint64
	r, _, _ := procConvertInterfaceAliasToLuid.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if r != 0 {
		return 0, fmt.Errorf("firewall: ConvertInterfaceAliasToLuid(%s): 0x%08x", name, r)
	}
	return luid, nil
}

// utf16Ptr is a helper to create a *uint16 from a Go string.
func utf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}
