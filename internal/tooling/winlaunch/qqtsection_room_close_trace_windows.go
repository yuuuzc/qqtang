package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtSectionRoomCloseRVA        = uintptr(0xAEB9)
	qqtSectionRoomClosePatchSize  = 6
	qqtSectionRoomCloseCapacity   = 128
	qqtSectionRoomCloseRecordSize = 24
	qqtSectionRoomCloseHeaderRVA  = uintptr(0xE0)
	qqtSectionRoomCloseRecordsRVA = uintptr(0x100)
)

var qqtSectionRoomCloseSignature = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x18}

// QQTSectionRoomCloseCall records one native QQTSection room-close call.
// Argument is the first stack argument: zero performs local cleanup only;
// non-zero also emits REQUEST_LEAVE_ROOM before destroying the local scene.
type QQTSectionRoomCloseCall struct {
	Index        uint32 `json:"index"`
	ThreadID     uint32 `json:"thread_id"`
	Caller       string `json:"caller"`
	CallerModule string `json:"caller_module,omitempty"`
	CallerRVA    string `json:"caller_rva,omitempty"`
	This         string `json:"this"`
	Argument     uint32 `json:"argument"`
}

type QQTSectionRoomCloseCapture struct {
	PID         uint32                    `json:"pid"`
	ElapsedMS   int64                     `json:"elapsed_ms"`
	Entry       string                    `json:"entry"`
	Observed    uint32                    `json:"observed"`
	Dropped     uint32                    `json:"dropped"`
	Calls       []QQTSectionRoomCloseCall `json:"calls,omitempty"`
	OriginalHex string                    `json:"original_hex"`
	Behavior    string                    `json:"behavior"`
}

// QQTSectionRoomCloseTracer is an opt-in diagnostic hook. It records the
// caller, object and argument, executes the exact overwritten prologue, and
// resumes the original function. It does not change the argument or result.
type QQTSectionRoomCloseTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	entry    uintptr
	resume   uintptr
	page     uintptr
	header   uintptr
	records  uintptr
	original []byte
	patch    []byte
	// The first diagnostic revision accidentally placed its counter over the
	// first instruction. Keep a recovery-only baseline so already-captured
	// calls can be salvaged and their entry hook removed.
	legacyInlineCounter bool
}

func InstallQQTSectionRoomCloseTracer(pid uint32, timeout time.Duration) (*QQTSectionRoomCloseTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTSectionRoomCloseTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	base, err := waitForModule(process, pid, "QQTSection.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.entry = base + qqtSectionRoomCloseRVA
	tracer.resume = tracer.entry + qqtSectionRoomClosePatchSize
	tracer.original, _ = readRemote(process, tracer.entry, qqtSectionRoomClosePatchSize)
	if !equalBytes(tracer.original, qqtSectionRoomCloseSignature) {
		// A diagnostic controller can be interrupted after installing the
		// transparent hook. Adopt that still-live page so its already-recorded
		// calls can be recovered and the original entry can be restored instead
		// of forcing the user to reproduce the match again.
		if len(tracer.original) != qqtSectionRoomClosePatchSize || tracer.original[0] != 0xe9 {
			return nil, fmt.Errorf("QQTSection room-close signature mismatch at 0x%08X: got %X", tracer.entry, tracer.original)
		}
		displacement := int32(binary.LittleEndian.Uint32(tracer.original[1:5]))
		page := uintptr(int64(tracer.entry+5) + int64(displacement))
		stubPrefix, ok := readRemote(process, page, 5)
		basePrefix := binary.LittleEndian.Uint32([]byte{0x9c, 0x60, 0xf0, 0xff})
		observed := uint32(0)
		if ok && len(stubPrefix) == 5 {
			observed = binary.LittleEndian.Uint32(stubPrefix[:4]) - basePrefix
		}
		if !ok || len(stubPrefix) != 5 || stubPrefix[4] != 0x05 || observed > qqtSectionRoomCloseCapacity {
			return nil, fmt.Errorf("QQTSection room-close entry points to an unknown hook at 0x%08X: entry=%X stub=%X", page, tracer.original, stubPrefix)
		}
		tracer.page = page
		tracer.header = page
		tracer.records = page + qqtSectionRoomCloseRecordsRVA
		tracer.legacyInlineCounter = true
		tracer.patch = append([]byte(nil), tracer.original...)
		tracer.original = append([]byte(nil), qqtSectionRoomCloseSignature...)
		failed = false
		return tracer, nil
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx QQTSection room-close trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.header = page + qqtSectionRoomCloseHeaderRVA
	tracer.records = page + qqtSectionRoomCloseRecordsRVA
	stub := buildQQTSectionRoomCloseTraceStub(tracer.header, tracer.records, tracer.resume)
	if len(stub) >= int(qqtSectionRoomCloseRecordsRVA) {
		return nil, fmt.Errorf("QQTSection room-close trace stub is unexpectedly large: %d", len(stub))
	}
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTSection room-close trace stub: %w", err)
	}
	tracer.patch = relativeJumpPatch(tracer.entry, page, qqtSectionRoomClosePatchSize)
	if !ensureRemoteBytes(process, tracer.entry, tracer.patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTSection room-close entry at 0x%08X", tracer.entry)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.entry, uintptr(len(tracer.patch)))
	failed = false
	return tracer, nil
}

func buildQQTSectionRoomCloseTraceStub(header, records, resume uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(header+4)) // active++
	stub = append(stub, 0xB8, 0x01, 0, 0, 0, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(header)) // old counter -> eax
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionRoomCloseCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)
	fullJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionRoomCloseRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(records))
	// [saved stack+36] caller, +24 saved ECX, +40 first argument.
	for _, field := range []struct {
		stackOffset byte
		record      byte
	}{{36, 4}, {24, 8}, {40, 12}} {
		stub = append(stub, 0x8B, 0x54, 0x24, field.stackOffset, 0x89, 0x50, field.record)
	}
	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x50, 0x10)
	stub = append(stub, 0xC7, 0x40, 0x14)
	stub = binary.LittleEndian.AppendUint32(stub, 1) // committed
	stub = append(stub, 0xC7, 0x00)
	stub = binary.LittleEndian.AppendUint32(stub, 1)
	finish := len(stub)
	patchNearJump(stub, fullJump, finish)
	stub = append(stub, 0xF0, 0xFF, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(header+4)) // active--
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, qqtSectionRoomCloseSignature...)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(resume))
	stub = append(stub, 0xC3)
	return stub
}

func (tracer *QQTSectionRoomCloseTracer) Capture(duration time.Duration) QQTSectionRoomCloseCapture {
	if duration <= 0 {
		duration = 20 * time.Minute
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 100)
		if waitResult == waitObject0 {
			break
		}
	}
	return tracer.capture()
}

func (tracer *QQTSectionRoomCloseTracer) capture() QQTSectionRoomCloseCapture {
	result := QQTSectionRoomCloseCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		Entry: fmt.Sprintf("0x%08X", tracer.entry), OriginalHex: fmt.Sprintf("%X", tracer.original),
		Behavior: "read-only QQTSection room-close entry trace; records caller/this/argument, executes the exact original prologue and resumes without changing client state",
	}
	header, ok := readRemote(tracer.process, tracer.header, 8)
	if !ok {
		return result
	}
	result.Observed = binary.LittleEndian.Uint32(header[0:4])
	if tracer.legacyInlineCounter {
		result.Observed -= binary.LittleEndian.Uint32([]byte{0x9c, 0x60, 0xf0, 0xff})
	}
	count := result.Observed
	if count > qqtSectionRoomCloseCapacity {
		result.Dropped = count - qqtSectionRoomCloseCapacity
		count = qqtSectionRoomCloseCapacity
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record, valid := readRemote(tracer.process, tracer.records+uintptr(index)*qqtSectionRoomCloseRecordSize, qqtSectionRoomCloseRecordSize)
		if !valid || binary.LittleEndian.Uint32(record[0:4]) == 0 || binary.LittleEndian.Uint32(record[20:24]) == 0 {
			continue
		}
		caller := binary.LittleEndian.Uint32(record[4:8])
		call := QQTSectionRoomCloseCall{
			Index: index, Caller: fmt.Sprintf("0x%08X", caller), This: formatTracePointer(record[8:12]),
			Argument: binary.LittleEndian.Uint32(record[12:16]), ThreadID: binary.LittleEndian.Uint32(record[16:20]),
		}
		for _, module := range modules {
			if caller >= module.base && caller-module.base < module.size {
				call.CallerModule = module.name
				call.CallerRVA = fmt.Sprintf("0x%X", caller-module.base)
				break
			}
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *QQTSectionRoomCloseTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.entry != 0 && len(tracer.original) != 0 && len(tracer.patch) != 0 {
		current, ok := readRemote(tracer.process, tracer.entry, len(tracer.patch))
		if ok && equalBytes(current, tracer.patch) {
			ensureRemoteBytes(tracer.process, tracer.entry, tracer.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), tracer.entry, uintptr(len(tracer.original)))
		}
	}
	if tracer.page != 0 && !tracer.legacyInlineCounter {
		idle := false
		for attempt := 0; attempt < 100; attempt++ {
			active, ok := readRemote(tracer.process, tracer.header+4, 4)
			if !ok || binary.LittleEndian.Uint32(active) == 0 {
				idle = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if idle {
			procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
		}
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
