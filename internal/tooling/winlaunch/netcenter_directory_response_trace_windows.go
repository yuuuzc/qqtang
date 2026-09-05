package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterDirectoryResponsePatchRVA   = uintptr(0x45F3)
	netCenterDirectoryResponseResumeRVA  = uintptr(0x4664)
	netCenterDirectoryDecryptedBufferRVA = uintptr(0xBA90D0)
)

var netCenterDirectoryResponseSignature = []byte{0x68, 0x96, 0xCA, 0x02, 0x00}

type NetCenterDirectoryResponseCapture struct {
	PID                 uint32   `json:"pid"`
	ElapsedMS           int64    `json:"elapsed_ms"`
	Calls               uint32   `json:"calls"`
	ThreadID            uint32   `json:"thread_id,omitempty"`
	PayloadLength       uint32   `json:"payload_length"`
	HandlerObject       string   `json:"handler_object"`
	FramePointer        string   `json:"frame_pointer"`
	Deserializer        string   `json:"deserializer"`
	DeserializerVTable  string   `json:"deserializer_vtable"`
	DeserializerMethods []string `json:"deserializer_methods,omitempty"`
	UICallback          string   `json:"ui_callback"`
	UICallbackVTable    string   `json:"ui_callback_vtable"`
	UICallbackMethods   []string `json:"ui_callback_methods,omitempty"`
	InnerHeaderHex      string   `json:"inner_header_hex"`
	DecryptedHex        string   `json:"decrypted_hex"`
	PatchAddress        string   `json:"patch_address"`
	StubAddress         string   `json:"stub_address"`
	OriginalBytes       string   `json:"original_bytes"`
	Behavior            string   `json:"behavior"`
}

type NetCenterDirectoryResponseTracer struct {
	pid             uint32
	process         syscall.Handle
	started         time.Time
	moduleBase      uintptr
	patchAddress    uintptr
	stubAddress     uintptr
	recordAddress   uintptr
	decryptedBuffer uintptr
	original        []byte
}

func InstallNetCenterDirectoryResponseTracer(pid uint32, timeout time.Duration) (*NetCenterDirectoryResponseTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterDirectoryResponseTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "NetCenter.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.moduleBase = moduleBase
	tracer.patchAddress = moduleBase + netCenterDirectoryResponsePatchRVA
	tracer.decryptedBuffer = moduleBase + netCenterDirectoryDecryptedBufferRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(netCenterDirectoryResponseSignature))
	if len(tracer.original) != len(netCenterDirectoryResponseSignature) ||
		!equalBytes(tracer.original, netCenterDirectoryResponseSignature) {
		return nil, fmt.Errorf("NetCenter directory-response signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter directory-response trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.recordAddress = page + 0x300
	stub := buildNetCenterDirectoryResponseTraceStub(page, tracer.recordAddress, moduleBase+netCenterDirectoryResponseResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write NetCenter directory-response trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch NetCenter directory-response branch at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildNetCenterDirectoryResponseTraceStub(page, record, resume uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // calls
	stub = append(stub, 0x8B, 0x04, 0x24, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // saved EDI / payload length
	stub = append(stub, 0x8B, 0x44, 0x24, 0x04, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // saved ESI / handler object
	stub = append(stub, 0x8B, 0x54, 0x24, 0x08, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16)) // saved EBP
	stub = append(stub, 0x8B, 0x42, 0x10, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+20)) // deserializer
	stub = append(stub, 0x8B, 0x42, 0x14, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+24)) // UI callback
	// Copy the normalized 14-byte inner header plus two adjacent bytes while the
	// large handler frame is guaranteed live.
	for offset := int8(-0x14); offset <= -0x08; offset += 4 {
		stub = append(stub, 0x8B, 0x42, byte(offset), 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+28+uintptr(int(offset+0x14))))
	}
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread id
	stub = append(stub, 0x61, 0x9D)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func (tracer *NetCenterDirectoryResponseTracer) CaptureUntilFirst(duration time.Duration) NetCenterDirectoryResponseCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.recordAddress, 44)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.recordAddress, 44)
	return tracer.capture(record)
}

func (tracer *NetCenterDirectoryResponseTracer) capture(record []byte) NetCenterDirectoryResponseCapture {
	result := NetCenterDirectoryResponseCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", tracer.patchAddress), StubAddress: fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "0x0133 only: record post-decrypt state before schema 0x0816, then skip deserialization/UI callback and return handled",
	}
	if len(record) != 44 {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.PayloadLength = binary.LittleEndian.Uint32(record[8:12])
	result.HandlerObject = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20]))
	deserializer := binary.LittleEndian.Uint32(record[20:24])
	callback := binary.LittleEndian.Uint32(record[24:28])
	result.Deserializer = fmt.Sprintf("0x%08X", deserializer)
	result.UICallback = fmt.Sprintf("0x%08X", callback)
	result.InnerHeaderHex = fmt.Sprintf("%X", record[28:44])
	result.DeserializerVTable, result.DeserializerMethods = tracer.readInterface(deserializer, 16)
	result.UICallbackVTable, result.UICallbackMethods = tracer.readInterface(callback, 16)
	readLength := int(result.PayloadLength) + 14
	if readLength > 526 {
		readLength = 526
	}
	if readLength >= 14 {
		if decrypted, ok := readRemote(tracer.process, tracer.decryptedBuffer, readLength); ok {
			result.DecryptedHex = fmt.Sprintf("%X", decrypted)
		}
	}
	return result
}

func (tracer *NetCenterDirectoryResponseTracer) readInterface(object uint32, methodCount int) (string, []string) {
	if object == 0 {
		return "0x00000000", nil
	}
	pointer, ok := readRemote(tracer.process, uintptr(object), 4)
	if !ok {
		return "0x00000000", nil
	}
	vtable := binary.LittleEndian.Uint32(pointer)
	methods, ok := readRemote(tracer.process, uintptr(vtable), methodCount*4)
	if !ok {
		return fmt.Sprintf("0x%08X", vtable), nil
	}
	result := make([]string, 0, methodCount)
	for offset := 0; offset < len(methods); offset += 4 {
		result = append(result, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(methods[offset:offset+4])))
	}
	return fmt.Sprintf("0x%08X", vtable), result
}

func (tracer *NetCenterDirectoryResponseTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
