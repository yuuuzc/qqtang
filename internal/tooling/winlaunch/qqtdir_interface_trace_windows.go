package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtModulesDirectoryInterfaceCallRVA   = uintptr(0x9212)
	qqtModulesDirectoryInterfaceResumeRVA = uintptr(0x9217)
)

var qqtModulesDirectoryInterfaceSignature = []byte{0x8B, 0x11, 0xFF, 0x52, 0x10}

type QQTDirInterfaceCapture struct {
	PID           uint32   `json:"pid"`
	ElapsedMS     int64    `json:"elapsed_ms"`
	Calls         uint32   `json:"calls"`
	ThreadID      uint32   `json:"thread_id,omitempty"`
	Object        string   `json:"object,omitempty"`
	VTable        string   `json:"vtable,omitempty"`
	Methods       []string `json:"methods,omitempty"`
	PatchAddress  string   `json:"patch_address"`
	StubAddress   string   `json:"stub_address"`
	OriginalBytes string   `json:"original_bytes"`
	Behavior      string   `json:"behavior"`
}

// QQTDirInterfaceTracer records the directory interface immediately before
// QQTModules invokes its verified vtable+0x10 initialization method. The five
// displaced instructions are replayed unchanged.
type QQTDirInterfaceTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallQQTDirInterfaceTracer(pid uint32, timeout time.Duration) (*QQTDirInterfaceTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTDirInterfaceTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.patchAddress = moduleBase + qqtModulesDirectoryInterfaceCallRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(qqtModulesDirectoryInterfaceSignature))
	if len(tracer.original) != len(qqtModulesDirectoryInterfaceSignature) || !equalBytes(tracer.original, qqtModulesDirectoryInterfaceSignature) {
		return nil, fmt.Errorf("QQTModules directory-interface signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTDir interface trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x200
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+4))
	stub = append(stub, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+8)) // object ECX
	stub = append(stub, 0x8B, 0x01, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+12)) // vtable
	stub = append(stub, 0x8B, 0x50, 0x10, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+16))
	stub = append(stub, 0x8B, 0x50, 0x14, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+20))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, tracer.original...)
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+qqtModulesDirectoryInterfaceResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTDir interface trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTModules directory-interface call at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func (tracer *QQTDirInterfaceTracer) CaptureUntilFirst(duration time.Duration) QQTDirInterfaceCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, 24)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, 24)
	return tracer.capture(record)
}

func (tracer *QQTDirInterfaceTracer) capture(record []byte) QQTDirInterfaceCapture {
	result := QQTDirInterfaceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", tracer.patchAddress), StubAddress: fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent QQTModules call of QQTDir interface vtable+0x10; record object and vtable targets",
	}
	if len(record) != 24 {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	object := binary.LittleEndian.Uint32(record[8:12])
	vtable := binary.LittleEndian.Uint32(record[12:16])
	result.Object = fmt.Sprintf("0x%08X", object)
	result.VTable = fmt.Sprintf("0x%08X", vtable)
	if vtable != 0 {
		methods, ok := readRemote(tracer.process, uintptr(vtable), 32*4)
		if ok {
			for offset := 0; offset < len(methods); offset += 4 {
				result.Methods = append(result.Methods, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(methods[offset:offset+4])))
			}
		}
	}
	return result
}

func (tracer *QQTDirInterfaceTracer) Close() {
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
