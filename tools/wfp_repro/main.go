//go:build wfp_diagnostic

// Repro tool used to diagnose the WFP enumFiltersByProvider bug
// (FWP_E_NEVER_MATCH on any non-NULL FWPM_FILTER_ENUM_TEMPLATE0). Excluded
// from regular builds via the wfp_diagnostic build tag — opt in with:
//
//	go build -tags wfp_diagnostic -o wfp_repro.exe ./tools/wfp_repro
//
// The repro:
// 1. Open WFP, add provider + sublayer (weight 0 — no effect on traffic).
// 2. Add 4 PERMIT filters: 2 with no conditions, 2 with conditions.
// 3. Re-open WFP, try several enum-template combinations to find which one
//    returns all 4 filters.
// 4. Delete provider/sublayer/filters before exit (cleanup, idempotent).
//
// Safe to run on a live machine: PERMIT filters at weight 0 in a low-weight
// sublayer cannot block any traffic.
package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modfwpuclnt                     = syscall.NewLazyDLL("fwpuclnt.dll")
	procFwpmEngineOpen0             = modfwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0            = modfwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmTransactionBegin0       = modfwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0      = modfwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0       = modfwpuclnt.NewProc("FwpmTransactionAbort0")
	procFwpmProviderAdd0            = modfwpuclnt.NewProc("FwpmProviderAdd0")
	procFwpmProviderDeleteByKey0    = modfwpuclnt.NewProc("FwpmProviderDeleteByKey0")
	procFwpmSubLayerAdd0            = modfwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmSubLayerDeleteByKey0    = modfwpuclnt.NewProc("FwpmSubLayerDeleteByKey0")
	procFwpmFilterAdd0              = modfwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmFilterDeleteById0       = modfwpuclnt.NewProc("FwpmFilterDeleteById0")
	procFwpmFilterCreateEnumHandle0 = modfwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0             = modfwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modfwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0             = modfwpuclnt.NewProc("FwpmFreeMemory0")
)

var (
	providerKey = windows.GUID{
		Data1: 0x3c5e8a9f, Data2: 0x8a3d, Data3: 0x4f1e,
		Data4: [8]byte{0x9b, 0x5c, 0x6d, 0x8a, 0x2f, 0x1e, 0x3b, 0x5a},
	}
	sublayerKey = windows.GUID{
		Data1: 0x7b3d5e1a, Data2: 0xc8f2, Data3: 0x4a6d,
		Data4: [8]byte{0xbe, 0x91, 0x3f, 0x5c, 0x8a, 0x2d, 0x1e, 0x4b},
	}
	layerALEAuthConnectV4 = windows.GUID{
		Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33,
		Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82},
	}
	layerALEAuthRecvAcceptV4 = windows.GUID{
		Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273,
		Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50},
	}
	conditionIPProto = windows.GUID{
		Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a,
		Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7},
	}
	conditionIPRemotePort = windows.GUID{
		Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a,
		Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b},
	}
)

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
type fwpmAction0 struct {
	actionType uint32
	filterType windows.GUID
}
type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	_              [4]byte
	conditionValue fwpValue0
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
	providerKey             *windows.GUID
	layerKey                windows.GUID
	enumType                uint32
	flags                   uint32
	providerContextTemplate uintptr
	numFilterConditions     uint32
	_pad                    [4]byte
	filterCondition         *fwpmFilterCondition0
	actionMask              uint32
	_pad2                   [4]byte
	calloutKey              *windows.GUID
}

const (
	fwpActionPermit = 0x00001002
	fwpUint8        = 1
	fwpUint16       = 2
	fwpMatchEqual   = 0
	ipprotoUDP      = 17
)

func utf16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func openEngine() (uintptr, error) {
	var h windows.Handle
	r, _, _ := procFwpmEngineOpen0.Call(0, 10, 0, 0, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, fmt.Errorf("EngineOpen: 0x%08x", r)
	}
	return uintptr(h), nil
}

// cleanup uses the only working enum strategy: NULL template + manual provider
// filter, then delete each matching filter ID.
func cleanup(eng uintptr) int {
	procFwpmTransactionBegin0.Call(eng, 0)

	var enumH windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(eng, 0, uintptr(unsafe.Pointer(&enumH)))
	deleted := 0
	if r == 0 {
		var ids []uint64
		for {
			var entries **fwpmFilter0
			var count uint32
			r, _, _ = procFwpmFilterEnum0.Call(eng, uintptr(enumH), 1000, uintptr(unsafe.Pointer(&entries)), uintptr(unsafe.Pointer(&count)))
			if r != 0 || count == 0 {
				break
			}
			ptrs := unsafe.Slice(entries, count)
			for _, fp := range ptrs {
				if fp.providerKey != nil && *fp.providerKey == providerKey {
					ids = append(ids, fp.filterId)
				}
			}
			procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
		}
		procFwpmFilterDestroyEnumHandle0.Call(eng, uintptr(enumH))
		for _, id := range ids {
			procFwpmFilterDeleteById0.Call(eng, uintptr(id))
			deleted++
		}
	}

	procFwpmSubLayerDeleteByKey0.Call(eng, uintptr(unsafe.Pointer(&sublayerKey)))
	procFwpmProviderDeleteByKey0.Call(eng, uintptr(unsafe.Pointer(&providerKey)))

	procFwpmTransactionCommit0.Call(eng)
	return deleted
}

func addFilter(eng uintptr, layer windows.GUID, name string, conds []fwpmFilterCondition0) (uint64, uint32) {
	pkf := providerKey
	f := fwpmFilter0{
		displayData: fwpmDisplayData0{name: utf16(name)},
		providerKey: &pkf,
		layerKey:    layer,
		subLayerKey: sublayerKey,
		weight:      fwpValue0{dataType: fwpUint8, value: 0},
		action:      fwpmAction0{actionType: fwpActionPermit},
	}
	if len(conds) > 0 {
		f.numFilterConditions = uint32(len(conds))
		f.filterCondition = &conds[0]
	}
	var fid uint64
	r, _, _ := procFwpmFilterAdd0.Call(eng, uintptr(unsafe.Pointer(&f)), 0, uintptr(unsafe.Pointer(&fid)))
	runtime.KeepAlive(&f)
	runtime.KeepAlive(&conds)
	return fid, uint32(r)
}

func enumCountNull(eng uintptr, label string) {
	var enumH windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(eng, 0, uintptr(unsafe.Pointer(&enumH)))
	if r != 0 {
		fmt.Printf("  %-50s CreateEnumHandle FAIL 0x%08x\n", label, r)
		return
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(eng, uintptr(enumH))
	total := 0
	ourMatches := 0
	for {
		var entries **fwpmFilter0
		var count uint32
		r, _, _ = procFwpmFilterEnum0.Call(eng, uintptr(enumH), 1000, uintptr(unsafe.Pointer(&entries)), uintptr(unsafe.Pointer(&count)))
		if r != 0 || count == 0 {
			break
		}
		ptrs := unsafe.Slice(entries, count)
		for _, fp := range ptrs {
			total++
			if fp.providerKey != nil && *fp.providerKey == providerKey {
				ourMatches++
			}
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
	}
	fmt.Printf("  %-50s scanned %d filters, %d matched our providerKey\n", label, total, ourMatches)
}

func enumCount(eng uintptr, label string, tmpl *fwpmFilterEnumTemplate0) {
	var enumH windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(eng, uintptr(unsafe.Pointer(tmpl)), uintptr(unsafe.Pointer(&enumH)))
	if r != 0 {
		fmt.Printf("  %-50s CreateEnumHandle FAIL 0x%08x\n", label, r)
		return
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(eng, uintptr(enumH))
	total := 0
	var ids []uint64
	for {
		var entries **fwpmFilter0
		var count uint32
		r, _, _ = procFwpmFilterEnum0.Call(eng, uintptr(enumH), 100, uintptr(unsafe.Pointer(&entries)), uintptr(unsafe.Pointer(&count)))
		if r != 0 {
			fmt.Printf("  %-50s Enum FAIL 0x%08x\n", label, r)
			return
		}
		if count == 0 {
			break
		}
		ptrs := unsafe.Slice(entries, count)
		for _, fp := range ptrs {
			ids = append(ids, fp.filterId)
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
		total += int(count)
	}
	fmt.Printf("  %-50s found %d filters %v\n", label, total, ids)
}

func main() {
	eng, err := openEngine()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer procFwpmEngineClose0.Call(eng)

	fmt.Println("=== pre-cleanup ===")
	n := cleanup(eng)
	fmt.Printf("  pre-cleanup: deleted %d residual filter(s)\n", n)

	fmt.Println("=== add 4 PERMIT filters in a transaction ===")
	r, _, _ := procFwpmTransactionBegin0.Call(eng, 0)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "TxBegin: 0x%08x\n", r)
		os.Exit(2)
	}
	committed := false
	defer func() {
		if !committed {
			procFwpmTransactionAbort0.Call(eng)
		}
	}()

	prov := fwpmProvider0{providerKey: providerKey, displayData: fwpmDisplayData0{name: utf16("wfp_repro_p")}}
	r, _, _ = procFwpmProviderAdd0.Call(eng, uintptr(unsafe.Pointer(&prov)), 0)
	if r != 0 && r != 0x80320009 {
		fmt.Fprintf(os.Stderr, "ProvAdd: 0x%08x\n", r)
		os.Exit(2)
	}
	pk := providerKey
	sl := fwpmSublayer0{
		subLayerKey: sublayerKey,
		displayData: fwpmDisplayData0{name: utf16("wfp_repro_sl")},
		providerKey: &pk,
		weight:      0, // lowest priority — purely diagnostic, no traffic effect
	}
	r, _, _ = procFwpmSubLayerAdd0.Call(eng, uintptr(unsafe.Pointer(&sl)), 0)
	if r != 0 && r != 0x80320009 {
		fmt.Fprintf(os.Stderr, "SubAdd: 0x%08x\n", r)
		os.Exit(2)
	}

	// Filter 1: V4 Connect, no conditions
	id, rc := addFilter(eng, layerALEAuthConnectV4, "f1_v4connect_nocond", nil)
	fmt.Printf("  f1 v4connect nocond  id=%d rc=0x%08x\n", id, rc)
	// Filter 2: V4 RecvAccept, no conditions
	id, rc = addFilter(eng, layerALEAuthRecvAcceptV4, "f2_v4recv_nocond", nil)
	fmt.Printf("  f2 v4recv    nocond  id=%d rc=0x%08x\n", id, rc)
	// Filter 3: V4 Connect, 2 conditions (proto + port)
	conds3 := []fwpmFilterCondition0{
		{fieldKey: conditionIPProto, matchType: fwpMatchEqual, conditionValue: fwpValue0{dataType: fwpUint8, value: ipprotoUDP}},
		{fieldKey: conditionIPRemotePort, matchType: fwpMatchEqual, conditionValue: fwpValue0{dataType: fwpUint16, value: 443}},
	}
	id, rc = addFilter(eng, layerALEAuthConnectV4, "f3_v4connect_protoport", conds3)
	fmt.Printf("  f3 v4connect cond    id=%d rc=0x%08x\n", id, rc)
	// Filter 4: V4 RecvAccept, 1 condition (port only)
	conds4 := []fwpmFilterCondition0{
		{fieldKey: conditionIPRemotePort, matchType: fwpMatchEqual, conditionValue: fwpValue0{dataType: fwpUint16, value: 443}},
	}
	id, rc = addFilter(eng, layerALEAuthRecvAcceptV4, "f4_v4recv_port", conds4)
	fmt.Printf("  f4 v4recv    cond    id=%d rc=0x%08x\n", id, rc)

	r, _, _ = procFwpmTransactionCommit0.Call(eng)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "TxCommit: 0x%08x\n", r)
		os.Exit(2)
	}
	committed = true
	fmt.Println("=== committed; now testing enum strategies on a fresh engine handle ===")

	eng2, err := openEngine()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer procFwpmEngineClose0.Call(eng2)

	pk2 := providerKey

	fmt.Printf("sizeof(fwpmFilterEnumTemplate0)=%d (expected 72)\n", unsafe.Sizeof(fwpmFilterEnumTemplate0{}))
	fmt.Printf("offsetof(layerKey)=%d (expected 8)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.layerKey))
	fmt.Printf("offsetof(enumType)=%d (expected 24)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.enumType))
	fmt.Printf("offsetof(flags)=%d (expected 28)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.flags))
	fmt.Printf("offsetof(providerContextTemplate)=%d (expected 32)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.providerContextTemplate))
	fmt.Printf("offsetof(numFilterConditions)=%d (expected 40)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.numFilterConditions))
	fmt.Printf("offsetof(filterCondition)=%d (expected 48)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.filterCondition))
	fmt.Printf("offsetof(actionMask)=%d (expected 56)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.actionMask))
	fmt.Printf("offsetof(calloutKey)=%d (expected 64)\n", unsafe.Offsetof(fwpmFilterEnumTemplate0{}.calloutKey))

	dumpTemplate := func(label string, t *fwpmFilterEnumTemplate0) {
		b := (*[72]byte)(unsafe.Pointer(t))
		fmt.Printf("  template %s bytes: %x\n", label, b[:])
	}

	// Strategy A: provider only, default enumType=0 (FULLY_CONTAINED)
	tA := fwpmFilterEnumTemplate0{providerKey: &pk2}
	dumpTemplate("A", &tA)
	enumCount(eng2, "A: provider, FULLY_CONTAINED (=0)", &tA)

	// Strategy B: provider only, OVERLAPPING (=1)
	tB := fwpmFilterEnumTemplate0{providerKey: &pk2, enumType: 1}
	enumCount(eng2, "B: provider, OVERLAPPING (=1)", &tB)

	// Strategy C: NO provider, NO layer, OVERLAPPING — should match all
	tC := fwpmFilterEnumTemplate0{enumType: 1}
	enumCount(eng2, "C: nil-template, OVERLAPPING (=1)", &tC)

	// Strategy D: NO template at all (NULL)
	enumCountNull(eng2, "D: NULL template (no filtering)")

	// Strategy E: provider + flags=BEST_TERMINATING_MATCH (0x1)
	tE := fwpmFilterEnumTemplate0{providerKey: &pk2, flags: 0x1}
	enumCount(eng2, "E: provider, flags=BEST_TERMINATING (0x1)", &tE)

	// Strategy F: provider + actionMask=0xFFFFFFFF, OVERLAPPING — match any action
	tF := fwpmFilterEnumTemplate0{providerKey: &pk2, enumType: 1, actionMask: 0xFFFFFFFF}
	enumCount(eng2, "F: provider, OVERLAPPING, actionMask=ALL", &tF)

	// Strategy G: provider + specific layer (V4 connect), OVERLAPPING
	tG := fwpmFilterEnumTemplate0{providerKey: &pk2, enumType: 1, layerKey: layerALEAuthConnectV4}
	enumCount(eng2, "G: provider, OVERLAPPING, layer=V4Connect", &tG)

	fmt.Println("=== cleanup ===")
	n2 := cleanup(eng2)
	fmt.Printf("  cleanup: deleted %d filter(s)\n", n2)
	fmt.Println("done")
}
