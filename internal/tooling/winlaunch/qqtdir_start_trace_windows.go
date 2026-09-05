package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtModulesDirectoryStartCallRVA   = uintptr(0x9228)
	qqtModulesDirectoryStartResumeRVA = uintptr(0x922E)
)

var qqtModulesDirectoryStartSignature = []byte{0x8B, 0x08, 0x50, 0xFF, 0x51, 0x14}

type QQTDirStartCapture struct {
	PID              uint32   `json:"pid"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	Calls            uint32   `json:"calls"`
	ThreadID         uint32   `json:"thread_id,omitempty"`
	DirectoryObject  string   `json:"directory_object"`
	DirectoryVTable  string   `json:"directory_vtable"`
	DirectoryMethods []string `json:"directory_methods,omitempty"`
	CallbackObject   string   `json:"callback_object"`
	CallbackVTable   string   `json:"callback_vtable"`
	CallbackMethods  []string `json:"callback_methods,omitempty"`
	PatchAddress     string   `json:"patch_address"`
	StubAddress      string   `json:"stub_address"`
	OriginalBytes    string   `json:"original_bytes"`
	Behavior         string   `json:"behavior"`
}

// QQTDirStartTracer records the two objects passed to QQTDir's verified
// vtable+0x14 start method. The displaced instructions are replayed unchanged.
type QQTDirStartTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallQQTDirStartTracer(pid uint32, timeout time.Duration) (*QQTDirStartTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTDirStartTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "QQTModules.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + qqtModulesDirectoryStartCallRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(qqtModulesDirectoryStartSignature))
	if len(tracer.original) != len(qqtModulesDirectoryStartSignature) ||
		!equalBytes(tracer.original, qqtModulesDirectoryStartSignature) {
		return nil, fmt.Errorf("QQTModules directory-start signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTDir start trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x200
	stub := buildQQTDirStartTraceStub(page, tracer.record, moduleBase+qqtModulesDirectoryStartResumeRVA, tracer.original)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTDir start trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTModules directory-start call at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildQQTDirStartTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // directory object EAX
	stub = append(stub, 0x8B, 0x54, 0x24, 0x24, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // callback from original [esp]
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func (tracer *QQTDirStartTracer) CaptureUntilFirst(duration time.Duration) QQTDirStartCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, 16)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, 16)
	return tracer.capture(record)
}

func (tracer *QQTDirStartTracer) capture(record []byte) QQTDirStartCapture {
	result := QQTDirStartCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", tracer.patchAddress), StubAddress: fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent QQTModules call of QQTDir vtable+0x14; record directory and host callback objects",
	}
	if len(record) != 16 {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	directoryObject := binary.LittleEndian.Uint32(record[8:12])
	callbackObject := binary.LittleEndian.Uint32(record[12:16])
	directoryVTable := tracer.readPointer(directoryObject)
	callbackVTable := tracer.readPointer(callbackObject)
	result.DirectoryObject = fmt.Sprintf("0x%08X", directoryObject)
	result.DirectoryVTable = fmt.Sprintf("0x%08X", directoryVTable)
	result.CallbackObject = fmt.Sprintf("0x%08X", callbackObject)
	result.CallbackVTable = fmt.Sprintf("0x%08X", callbackVTable)
	result.DirectoryMethods = tracer.readMethods(directoryVTable)
	result.CallbackMethods = tracer.readMethods(callbackVTable)
	return result
}

func (tracer *QQTDirStartTracer) readPointer(address uint32) uint32 {
	if address == 0 {
		return 0
	}
	value, ok := readRemote(tracer.process, uintptr(address), 4)
	if !ok {
		return 0
	}
	return binary.LittleEndian.Uint32(value)
}

func (tracer *QQTDirStartTracer) readMethods(vtable uint32) []string {
	if vtable == 0 {
		return nil
	}
	methods, ok := readRemote(tracer.process, uintptr(vtable), 32*4)
	if !ok {
		return nil
	}
	result := make([]string, 0, 32)
	for offset := 0; offset < len(methods); offset += 4 {
		result = append(result, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(methods[offset:offset+4])))
	}
	return result
}

func (tracer *QQTDirStartTracer) Close() {
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
