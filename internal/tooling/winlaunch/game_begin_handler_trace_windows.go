package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"syscall"
	"time"
)

const (
	qqtSectionGameBeginThunkRVA  = uintptr(0x15A0)
	qqtSectionGameBeginTargetRVA = uintptr(0xC0B4)
	qqtGameBeginNativeSize       = 0x5AE
)

type QQTSectionGameBeginCall struct {
	Calls       uint32 `json:"calls"`
	ThreadID    uint32 `json:"thread_id"`
	This        string `json:"this"`
	Object      string `json:"object"`
	GameID      uint32 `json:"game_id"`
	MapID       uint32 `json:"map_id"`
	MapHash     string `json:"map_hash"`
	ContinueID  int32  `json:"continue_id"`
	FileHash    string `json:"file_hash"`
	NativeBytes string `json:"native_bytes_head"`
}

type QQTSectionGameBeginCapture struct {
	PID       uint32                   `json:"pid"`
	ElapsedMS int64                    `json:"elapsed_ms"`
	Module    string                   `json:"module"`
	Thunk     string                   `json:"thunk"`
	Target    string                   `json:"target"`
	Call      *QQTSectionGameBeginCall `json:"call,omitempty"`
	Behavior  string                   `json:"behavior"`
}

type QQTSectionGameBeginTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	thunk    uintptr
	target   uintptr
	original []byte
	page     uintptr
	record   uintptr
}

func InstallQQTSectionGameBeginTracer(pid uint32, timeout time.Duration) (*QQTSectionGameBeginTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTSectionGameBeginTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.thunk = base + qqtSectionGameBeginThunkRVA
	tracer.target = base + qqtSectionGameBeginTargetRVA
	tracer.original, _ = readRemote(process, tracer.thunk, 5)
	if len(tracer.original) != 5 || tracer.original[0] != 0xE9 {
		return nil, fmt.Errorf("QQTSection game-begin thunk 0x%08X does not start with JMP rel32", tracer.thunk)
	}
	originalTarget := uintptr(int64(tracer.thunk+5) + int64(int32(binary.LittleEndian.Uint32(tracer.original[1:5]))))
	if originalTarget != tracer.target {
		return nil, fmt.Errorf("QQTSection game-begin thunk target 0x%08X, want 0x%08X", originalTarget, tracer.target)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx game-begin trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x300
	stub := buildQQTSectionGameBeginTraceStub(tracer.record, tracer.target, page)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write game-begin trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	displacement := int64(page) - int64(tracer.thunk+5)
	binary.LittleEndian.PutUint32(patch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, tracer.thunk, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTSection game-begin thunk 0x%08X", tracer.thunk)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.thunk, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildQQTSectionGameBeginTraceStub(record, target, stubAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread ID
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // saved ECX / this
	stub = append(stub, 0x8B, 0x44, 0x24, 0x28, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // first argument
	stub = append(stub, 0x8B, 0x74, 0x24, 0x28, 0xBF)                // mov esi,[esp+28h]; mov edi,imm32
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+0x20))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, qqtGameBeginNativeSize) // mov ecx,native size
	stub = append(stub, 0xFC, 0xF3, 0xA4)                                 // cld; rep movsb
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // publish completed capture
	stub = append(stub, 0x61, 0x9D, 0xE9)
	next := stubAddress + uintptr(len(stub)) + 4
	stub = binary.LittleEndian.AppendUint32(stub, uint32(int32(int64(target)-int64(next))))
	return stub
}

func (tracer *QQTSectionGameBeginTracer) Capture(duration time.Duration) QQTSectionGameBeginCapture {
	if duration <= 0 {
		duration = 20 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, 0x20+qqtGameBeginNativeSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture(nil)
}

func (tracer *QQTSectionGameBeginTracer) capture(record []byte) QQTSectionGameBeginCapture {
	result := QQTSectionGameBeginCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Module: "QQTSection.dll",
		Thunk: fmt.Sprintf("0x%08X", tracer.thunk), Target: fmt.Sprintf("0x%08X", tracer.target),
		Behavior: "transparent game-begin thunk trace; exact five-byte JMP restored on close",
	}
	if len(record) < 0x20+qqtGameBeginNativeSize {
		return result
	}
	object := uintptr(binary.LittleEndian.Uint32(record[12:16]))
	native := record[0x20 : 0x20+qqtGameBeginNativeSize]
	head := native
	if len(head) > 128 {
		head = head[:128]
	}
	result.Call = &QQTSectionGameBeginCall{
		Calls: binary.LittleEndian.Uint32(record[0:4]), ThreadID: binary.LittleEndian.Uint32(record[4:8]),
		This: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])), Object: fmt.Sprintf("0x%08X", object),
		GameID: binary.LittleEndian.Uint32(native[0:4]), MapID: binary.LittleEndian.Uint32(native[4:8]),
		MapHash:    strings.TrimRight(string(native[0x31D:0x33D]), "\x00"),
		ContinueID: int32(binary.LittleEndian.Uint32(native[0x596:0x59A])),
		FileHash:   hex.EncodeToString(native[0x59A:0x5AA]), NativeBytes: hex.EncodeToString(head),
	}
	return result
}

func (tracer *QQTSectionGameBeginTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.thunk != 0 && len(tracer.original) == 5 {
		ensureRemoteBytes(tracer.process, tracer.thunk, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.thunk, uintptr(len(tracer.original)))
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
