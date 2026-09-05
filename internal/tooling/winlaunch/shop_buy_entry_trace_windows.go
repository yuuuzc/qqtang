package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtShopBuyEntryRVA      = uintptr(0xB03E)
	qqtShopBuyInterfacesRVA = uintptr(0xB08D)
	qqtShopBuyDispatchRVA   = uintptr(0xB0D0)
	shopBuyEntryRecordSize  = 96
)

var (
	qqtShopBuyEntrySignature      = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x0C}
	qqtShopBuyInterfacesSignature = []byte{0x8B, 0x45, 0xF8, 0x3B, 0xC6}
	qqtShopBuyDispatchSignature   = []byte{0x8B, 0x45, 0xFC, 0x8D, 0x73, 0x0A}
)

type ShopBuyEntryTraceEvent struct {
	Calls            uint32   `json:"calls"`
	ThreadID         uint32   `json:"thread_id,omitempty"`
	ShopObject       string   `json:"shop_object"`
	Request          string   `json:"request"`
	InterfaceRoot    string   `json:"interface_root"`
	Busy             uint32   `json:"busy"`
	BusyResetState   uint32   `json:"busy_reset_state"`
	CommodityContext uint32   `json:"commodity_context"`
	FirstInterface   string   `json:"first_interface"`
	SecondInterface  string   `json:"second_interface"`
	SecondVTable     string   `json:"second_vtable"`
	DispatchTarget   string   `json:"dispatch_target"`
	RequestWords     []uint32 `json:"request_words,omitempty"`
}

type ShopBuyEntryTraceCapture struct {
	PID                uint32                 `json:"pid"`
	ElapsedMS          int64                  `json:"elapsed_ms"`
	Event              ShopBuyEntryTraceEvent `json:"event"`
	ModuleBase         string                 `json:"module_base"`
	EntryPatch         string                 `json:"entry_patch"`
	InterfacesPatch    string                 `json:"interfaces_patch"`
	DispatchPatch      string                 `json:"dispatch_patch"`
	OriginalEntry      string                 `json:"original_entry"`
	OriginalInterfaces string                 `json:"original_interfaces"`
	OriginalDispatch   string                 `json:"original_dispatch"`
	Behavior           string                 `json:"behavior"`
}

type ShopBuyEntryTracer struct {
	pid                                        uint32
	process                                    syscall.Handle
	started                                    time.Time
	page, record, moduleBase                   uintptr
	entryPatch, interfacesPatch, dispatchPatch uintptr
	entryOriginal, interfacesOriginal          []byte
	dispatchOriginal                           []byte
}

// InstallShopBuyEntryTracer observes the native QQTShop2ND Buy path without
// changing its return value, interface lookups, queue state, or dispatch.
func InstallShopBuyEntryTracer(pid uint32, timeout time.Duration) (*ShopBuyEntryTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ShopBuyEntryTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	base, err := waitForModule(process, pid, "QQTShop2ND.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.moduleBase = base
	tracer.entryPatch = base + qqtShopBuyEntryRVA
	tracer.interfacesPatch = base + qqtShopBuyInterfacesRVA
	tracer.dispatchPatch = base + qqtShopBuyDispatchRVA
	checks := []struct {
		name      string
		address   uintptr
		signature []byte
		out       *[]byte
	}{
		{"entry", tracer.entryPatch, qqtShopBuyEntrySignature, &tracer.entryOriginal},
		{"interfaces", tracer.interfacesPatch, qqtShopBuyInterfacesSignature, &tracer.interfacesOriginal},
		{"dispatch", tracer.dispatchPatch, qqtShopBuyDispatchSignature, &tracer.dispatchOriginal},
	}
	for _, check := range checks {
		got, ok := readRemote(process, check.address, len(check.signature))
		if !ok || !equalBytes(got, check.signature) {
			return nil, fmt.Errorf("QQTShop Buy %s signature mismatch at 0x%08X: got %X", check.name, check.address, got)
		}
		*check.out = got
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx shop Buy trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x600
	stubs := []struct {
		address uintptr
		data    []byte
	}{
		{page, buildShopBuyEntryStub(page, tracer.record, tracer.entryPatch, tracer.entryOriginal)},
		{page + 0x200, buildShopBuyInterfacesStub(page+0x200, tracer.record, tracer.interfacesPatch, tracer.interfacesOriginal)},
		{page + 0x400, buildShopBuyDispatchStub(page+0x400, tracer.record, tracer.dispatchPatch, tracer.dispatchOriginal)},
	}
	for _, stub := range stubs {
		if err := writeRemote(process, stub.address, stub.data); err != nil {
			return nil, fmt.Errorf("write shop Buy trace stub: %w", err)
		}
	}
	patches := []struct {
		address uintptr
		stub    uintptr
		size    int
	}{
		{tracer.entryPatch, page, len(tracer.entryOriginal)},
		{tracer.interfacesPatch, page + 0x200, len(tracer.interfacesOriginal)},
		{tracer.dispatchPatch, page + 0x400, len(tracer.dispatchOriginal)},
	}
	for _, patch := range patches {
		if err := patchShopDealTraceEntry(process, patch.address, patch.stub, patch.size); err != nil {
			return nil, err
		}
	}
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	failed = false
	return tracer, nil
}

func buildShopBuyEntryStub(stub, record, resume uintptr, original []byte) []byte {
	code := []byte{0x9C, 0x60}
	code = appendTraceIncrementAndThread(code, record)
	code = append(code, 0x8B, 0x44, 0x24, 0x18, 0xA3) // saved ECX
	code = binary.LittleEndian.AppendUint32(code, uint32(record+8))
	code = append(code, 0x8B, 0x54, 0x24, 0x0C, 0x8B, 0x42, 0x08, 0xA3) // arg1
	code = binary.LittleEndian.AppendUint32(code, uint32(record+12))
	code = append(code, 0x8B, 0x54, 0x24, 0x18)
	for _, field := range []struct {
		source byte
		target uintptr
	}{{0x18, 16}, {0x20, 20}, {0x30, 24}, {0xDC, 28}} {
		if field.source == 0xDC {
			code = append(code, 0x8B, 0x82, 0xDC, 0, 0, 0, 0xA3)
		} else {
			code = append(code, 0x8B, 0x42, field.source, 0xA3)
		}
		code = binary.LittleEndian.AppendUint32(code, uint32(record+field.target))
	}
	code = append(code, 0x61, 0x9D)
	code = append(code, original...)
	return appendRelativeJump(code, stub+uintptr(len(code)), resume+uintptr(len(original)))
}

func buildShopBuyInterfacesStub(stub, record, resume uintptr, original []byte) []byte {
	code := []byte{0x9C, 0x60, 0xF0, 0xFF, 0x05}
	code = binary.LittleEndian.AppendUint32(code, uint32(record+32))
	code = append(code, 0x8B, 0x44, 0x24, 0x08) // saved EBP
	code = append(code, 0x8B, 0x50, 0xF8, 0x89, 0x15)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+36))
	code = append(code, 0x8B, 0x50, 0xFC, 0x89, 0x15)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+40))
	code = append(code, 0x61, 0x9D)
	code = append(code, original...)
	return appendRelativeJump(code, stub+uintptr(len(code)), resume+uintptr(len(original)))
}

func buildShopBuyDispatchStub(stub, record, resume uintptr, original []byte) []byte {
	code := []byte{0x9C, 0x60, 0xF0, 0xFF, 0x05}
	code = binary.LittleEndian.AppendUint32(code, uint32(record+44))
	code = append(code, 0x8B, 0x44, 0x24, 0x08) // saved EBP
	code = append(code, 0x8B, 0x50, 0xFC, 0x89, 0x15)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+48))
	code = append(code, 0x8B, 0x0A, 0x89, 0x0D)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+52))
	code = append(code, 0x8B, 0x89, 0x10, 0x01, 0, 0, 0x89, 0x0D)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+56))
	code = append(code, 0x8B, 0x50, 0x08, 0x89, 0x15)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+12))
	// Copy the first six request dwords after QQTShop has filled its header.
	for offset := byte(0); offset < 24; offset += 4 {
		code = append(code, 0x8B, 0x4A, offset, 0x89, 0x0D)
		code = binary.LittleEndian.AppendUint32(code, uint32(record+60+uintptr(offset)))
	}
	code = append(code, 0x61, 0x9D)
	code = append(code, original...)
	return appendRelativeJump(code, stub+uintptr(len(code)), resume+uintptr(len(original)))
}

func appendTraceIncrementAndThread(code []byte, record uintptr) []byte {
	code = append(code, 0xF0, 0xFF, 0x05)
	code = binary.LittleEndian.AppendUint32(code, uint32(record))
	code = append(code, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	return binary.LittleEndian.AppendUint32(code, uint32(record+4))
}

func (tracer *ShopBuyEntryTracer) Capture(duration, settle time.Duration) ShopBuyEntryTraceCapture {
	if duration <= 0 {
		duration = 5 * time.Minute
	}
	if settle <= 0 {
		settle = 750 * time.Millisecond
	}
	deadline := time.Now().Add(duration)
	var capturedAt time.Time
	for time.Now().Before(deadline) {
		data, _ := readRemote(tracer.process, tracer.record, 4)
		if len(data) == 4 && binary.LittleEndian.Uint32(data) != 0 {
			if capturedAt.IsZero() {
				capturedAt = time.Now()
			}
			if time.Since(capturedAt) >= settle {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	data, _ := readRemote(tracer.process, tracer.record, shopBuyEntryRecordSize)
	return ShopBuyEntryTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Event: decodeShopBuyEntryTrace(data),
		ModuleBase: fmt.Sprintf("0x%08X", tracer.moduleBase), EntryPatch: fmt.Sprintf("0x%08X", tracer.entryPatch),
		InterfacesPatch: fmt.Sprintf("0x%08X", tracer.interfacesPatch), DispatchPatch: fmt.Sprintf("0x%08X", tracer.dispatchPatch),
		OriginalEntry: fmt.Sprintf("%X", tracer.entryOriginal), OriginalInterfaces: fmt.Sprintf("%X", tracer.interfacesOriginal),
		OriginalDispatch: fmt.Sprintf("%X", tracer.dispatchOriginal),
		Behavior:         "records QQTShop2ND Buy entry, interface lookup results, and native vtable+0x110 dispatch while replaying original instructions unchanged",
	}
}

func decodeShopBuyEntryTrace(data []byte) ShopBuyEntryTraceEvent {
	if len(data) < shopBuyEntryRecordSize {
		return ShopBuyEntryTraceEvent{}
	}
	ptr := func(offset int) string { return fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(data[offset:])) }
	words := make([]uint32, 6)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[60+index*4:])
	}
	return ShopBuyEntryTraceEvent{
		Calls: binary.LittleEndian.Uint32(data), ThreadID: binary.LittleEndian.Uint32(data[4:]),
		ShopObject: ptr(8), Request: ptr(12), InterfaceRoot: ptr(16), Busy: binary.LittleEndian.Uint32(data[20:]),
		BusyResetState: binary.LittleEndian.Uint32(data[24:]), CommodityContext: binary.LittleEndian.Uint32(data[28:]),
		FirstInterface: ptr(36), SecondInterface: ptr(40), SecondVTable: ptr(52), DispatchTarget: ptr(56), RequestWords: words,
	}
}

func (tracer *ShopBuyEntryTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for _, restore := range []struct {
		address  uintptr
		original []byte
	}{{tracer.entryPatch, tracer.entryOriginal}, {tracer.interfacesPatch, tracer.interfacesOriginal}, {tracer.dispatchPatch, tracer.dispatchOriginal}} {
		if restore.address != 0 && len(restore.original) != 0 {
			ensureRemoteBytes(tracer.process, restore.address, restore.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), restore.address, uintptr(len(restore.original)))
		}
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
