package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtShopResponseDispatchRVA = uintptr(0xA43C)
	qqtShopBuyDecodeRVA        = uintptr(0xA515)
	qqtShopBuyResultRVA        = uintptr(0xB412)
	shopResponseTraceDataSize  = 80
)

var (
	qqtShopResponseDispatchSignature = []byte{0x55, 0x8B, 0xEC, 0x8B, 0x55, 0x10, 0xB8, 0x63, 0x02, 0x00, 0x00}
	qqtShopBuyDecodeSignature        = []byte{0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x10, 0x01, 0x00, 0x00}
	qqtShopBuyResultSignature        = []byte{0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x08, 0x05, 0x00, 0x00}
)

type shopResponseTraceEntry uint8

const (
	shopResponseTraceDecode shopResponseTraceEntry = iota
	shopResponseTraceResult
	shopResponseTraceDispatch
)

// ShopResponseTraceCapture separates network delivery from QQTShop2ND's
// schema decode and UI result consumer. It is diagnostic only: both entry
// prologues are replayed byte-for-byte and restored by Close.
type ShopResponseTraceCapture struct {
	PID                   uint32 `json:"pid"`
	ElapsedMS             int64  `json:"elapsed_ms"`
	DispatchCalls         uint32 `json:"dispatch_calls"`
	PurchaseDispatchCalls uint32 `json:"purchase_dispatch_calls"`
	DecodeCalls           uint32 `json:"decode_calls"`
	ResultCalls           uint32 `json:"result_calls"`
	DispatchThreadID      uint32 `json:"dispatch_thread_id,omitempty"`
	DecodeThreadID        uint32 `json:"decode_thread_id,omitempty"`
	ResultThreadID        uint32 `json:"result_thread_id,omitempty"`
	DispatchObject        string `json:"dispatch_object,omitempty"`
	DispatchArgument1     string `json:"dispatch_argument_1,omitempty"`
	DispatchArgument2     string `json:"dispatch_argument_2,omitempty"`
	DispatchCommand       uint32 `json:"dispatch_command,omitempty"`
	DecodeObject          string `json:"decode_object,omitempty"`
	DecodeArgument1       string `json:"decode_argument_1,omitempty"`
	DecodeArgument2       string `json:"decode_argument_2,omitempty"`
	ResultObject          string `json:"result_object,omitempty"`
	ResultArgument        string `json:"result_argument,omitempty"`
	ResultPrefixHex       string `json:"result_prefix_hex,omitempty"`
	DispatchPatch         string `json:"dispatch_patch"`
	DecodePatch           string `json:"decode_patch"`
	ResultPatch           string `json:"result_patch"`
	Behavior              string `json:"behavior"`
}

type ShopResponseTracer struct {
	pid              uint32
	process          syscall.Handle
	started          time.Time
	page             uintptr
	record           uintptr
	dispatchPatch    uintptr
	decodePatch      uintptr
	resultPatch      uintptr
	dispatchOriginal []byte
	decodeOriginal   []byte
	resultOriginal   []byte
}

func InstallShopResponseTracer(pid uint32, timeout time.Duration) (*ShopResponseTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ShopResponseTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.dispatchPatch = base + qqtShopResponseDispatchRVA
	tracer.decodePatch = base + qqtShopBuyDecodeRVA
	tracer.resultPatch = base + qqtShopBuyResultRVA
	tracer.dispatchOriginal, _ = readRemote(process, tracer.dispatchPatch, len(qqtShopResponseDispatchSignature))
	tracer.decodeOriginal, _ = readRemote(process, tracer.decodePatch, len(qqtShopBuyDecodeSignature))
	tracer.resultOriginal, _ = readRemote(process, tracer.resultPatch, len(qqtShopBuyResultSignature))
	if !equalBytes(tracer.dispatchOriginal, qqtShopResponseDispatchSignature) {
		return nil, fmt.Errorf("QQTShop response dispatch signature mismatch at 0x%08X: got %X", tracer.dispatchPatch, tracer.dispatchOriginal)
	}
	if !equalBytes(tracer.decodeOriginal, qqtShopBuyDecodeSignature) {
		return nil, fmt.Errorf("QQTShop buy decode signature mismatch at 0x%08X: got %X", tracer.decodePatch, tracer.decodeOriginal)
	}
	if !equalBytes(tracer.resultOriginal, qqtShopBuyResultSignature) {
		return nil, fmt.Errorf("QQTShop buy result signature mismatch at 0x%08X: got %X", tracer.resultPatch, tracer.resultOriginal)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx shop response trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x500
	decodeStub := buildShopResponseEntryStub(page, tracer.record, tracer.decodePatch, tracer.decodeOriginal, shopResponseTraceDecode)
	resultStub := buildShopResponseEntryStub(page+0x180, tracer.record, tracer.resultPatch, tracer.resultOriginal, shopResponseTraceResult)
	dispatchStub := buildShopResponseEntryStub(page+0x300, tracer.record, tracer.dispatchPatch, tracer.dispatchOriginal, shopResponseTraceDispatch)
	if err := writeRemote(process, page, decodeStub); err != nil {
		return nil, fmt.Errorf("write shop response decode trace: %w", err)
	}
	if err := writeRemote(process, page+0x180, resultStub); err != nil {
		return nil, fmt.Errorf("write shop response result trace: %w", err)
	}
	if err := writeRemote(process, page+0x300, dispatchStub); err != nil {
		return nil, fmt.Errorf("write shop response dispatch trace: %w", err)
	}
	if err := patchShopDealTraceEntry(process, tracer.dispatchPatch, page+0x300, len(tracer.dispatchOriginal)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.decodePatch, page, len(tracer.decodeOriginal)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.resultPatch, page+0x180, len(tracer.resultOriginal)); err != nil {
		return nil, err
	}
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	failed = false
	return tracer, nil
}

func buildShopResponseEntryStub(stubAddress, record, patchAddress uintptr, original []byte, entry shopResponseTraceEntry) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	base := uintptr(0)
	switch entry {
	case shopResponseTraceResult:
		base = 20
	case shopResponseTraceDispatch:
		base = 52
	}
	stub = append(stub, 0xF0, 0xFF, 0x05) // lock inc dword ptr [calls]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+base))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3) // thread ID
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+base+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3) // saved ECX / this
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+base+8))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x28, 0xA3) // first stack argument
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+base+12))
	switch entry {
	case shopResponseTraceResult:
		// Preserve the first 16 bytes of the already decoded RESPONSE_BUY.
		stub = append(stub, 0x85, 0xC0, 0x74, 0x24) // test eax,eax; jz restore
		for offset := uint32(0); offset < 16; offset += 4 {
			stub = append(stub, 0x8B, 0x50, byte(offset), 0x89, 0x15)
			stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36+uintptr(offset)))
		}
	case shopResponseTraceDecode:
		stub = append(stub, 0x8B, 0x44, 0x24, 0x2C, 0xA3) // second stack argument
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16))
	case shopResponseTraceDispatch:
		stub = append(stub, 0x8B, 0x44, 0x24, 0x2C, 0xA3) // second stack argument
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+68))
		stub = append(stub, 0x8B, 0x44, 0x24, 0x30, 0xA3) // response command
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+72))
		stub = append(stub, 0x3D, 0x59, 0x02, 0x00, 0x00, 0x75, 0x07) // cmp eax,0x259; jne restore
		stub = append(stub, 0xF0, 0xFF, 0x05)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+76))
	}
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), patchAddress+uintptr(len(original)))
}

func (tracer *ShopResponseTracer) Capture(duration, settle time.Duration) ShopResponseTraceCapture {
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if settle <= 0 {
		settle = 300 * time.Millisecond
	}
	deadline := time.Now().Add(duration)
	var observedAt time.Time
	for time.Now().Before(deadline) {
		record, _ := readRemote(tracer.process, tracer.record, shopResponseTraceDataSize)
		if len(record) == shopResponseTraceDataSize && (binary.LittleEndian.Uint32(record[:4]) != 0 || binary.LittleEndian.Uint32(record[20:24]) != 0 || binary.LittleEndian.Uint32(record[76:80]) != 0) {
			if observedAt.IsZero() {
				observedAt = time.Now()
			}
			if time.Since(observedAt) >= settle {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, shopResponseTraceDataSize)
	return decodeShopResponseTrace(tracer, record)
}

func decodeShopResponseTrace(tracer *ShopResponseTracer, record []byte) ShopResponseTraceCapture {
	result := ShopResponseTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		DispatchPatch: fmt.Sprintf("0x%08X", tracer.dispatchPatch), DecodePatch: fmt.Sprintf("0x%08X", tracer.decodePatch), ResultPatch: fmt.Sprintf("0x%08X", tracer.resultPatch),
		Behavior: "transparent QQTShop2ND response dispatch/decode/result entry counters; original prologues restored on close",
	}
	if len(record) != shopResponseTraceDataSize {
		return result
	}
	result.DecodeCalls = binary.LittleEndian.Uint32(record[0:4])
	result.DecodeThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.DecodeObject = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[8:12])))
	result.DecodeArgument1 = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[12:16])))
	result.DecodeArgument2 = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[16:20])))
	result.ResultCalls = binary.LittleEndian.Uint32(record[20:24])
	result.ResultThreadID = binary.LittleEndian.Uint32(record[24:28])
	result.ResultObject = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[28:32])))
	result.ResultArgument = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[32:36])))
	result.ResultPrefixHex = fmt.Sprintf("%X", record[36:52])
	result.DispatchCalls = binary.LittleEndian.Uint32(record[52:56])
	result.DispatchThreadID = binary.LittleEndian.Uint32(record[56:60])
	result.DispatchObject = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[60:64])))
	result.DispatchArgument1 = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[64:68])))
	result.DispatchArgument2 = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(record[68:72])))
	result.DispatchCommand = binary.LittleEndian.Uint32(record[72:76])
	result.PurchaseDispatchCalls = binary.LittleEndian.Uint32(record[76:80])
	return result
}

func (tracer *ShopResponseTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for _, hook := range []struct {
		address  uintptr
		original []byte
	}{{tracer.dispatchPatch, tracer.dispatchOriginal}, {tracer.decodePatch, tracer.decodeOriginal}, {tracer.resultPatch, tracer.resultOriginal}} {
		if hook.address != 0 && len(hook.original) != 0 {
			ensureRemoteBytes(tracer.process, hook.address, hook.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), hook.address, uintptr(len(hook.original)))
		}
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
