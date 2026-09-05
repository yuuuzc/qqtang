package winlaunch

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	clientGameOverProcessRVA        = uintptr(0x202BAB)
	clientGameOverProcessTargetRVA  = uintptr(0x481E00)
	clientGameOverNativeSize        = 358
	clientGameOverTraceRecordOffset = 0x200
	clientGameOverTraceRecordSize   = 0x40 + clientGameOverNativeSize
)

var clientGameOverProcessSignature = []byte{0xE9, 0x50, 0xF2, 0x27, 0x00}

type ClientGameOverNativeResult struct {
	PlayerID    uint16   `json:"player_id"`
	Result      uint8    `json:"result"`
	Remark      uint32   `json:"remark"`
	Point       uint32   `json:"point"`
	FieldCount  uint8    `json:"field_count"`
	FieldValue1 []uint32 `json:"field_value_1"`
	FieldValue2 []uint32 `json:"field_value_2"`
}

// ClientGameOverFieldProbe is an opt-in, transient probe for decoded
// NOTIFY_GAME_OVER fields. It is applied only to the decoded object in
// Client.exe immediately before the original handler runs; it never changes
// the network packet, server state, or persisted profile.
type ClientGameOverFieldProbe struct {
	ReplaceFields bool      `json:"replace_fields,omitempty"`
	FieldValue1   [4]uint32 `json:"field_value_1,omitempty"`
	FieldValue2   [4]uint32 `json:"field_value_2,omitempty"`
	GameMode      *uint8    `json:"game_mode,omitempty"`
}

type ClientGameOverProcessCall struct {
	Calls               uint32                       `json:"calls"`
	ThreadID            uint32                       `json:"thread_id"`
	Caller              string                       `json:"caller"`
	This                string                       `json:"this"`
	SchemaID            uint32                       `json:"schema_id"`
	SchemaHex           string                       `json:"schema_hex"`
	NativeObject        string                       `json:"native_object"`
	StackAfterArguments string                       `json:"stack_after_arguments"`
	StackPointer        string                       `json:"stack_pointer"`
	Time                uint32                       `json:"time"`
	ResultCount         uint8                        `json:"result_count"`
	Results             []ClientGameOverNativeResult `json:"results"`
	GameMode            uint8                        `json:"game_mode"`
	NativeSHA256        string                       `json:"native_sha256"`
	NativeHex           string                       `json:"native_hex"`
}

type ClientGameOverProcessCapture struct {
	PID        uint32                     `json:"pid"`
	ElapsedMS  int64                      `json:"elapsed_ms"`
	Module     string                     `json:"module"`
	Entry      string                     `json:"entry"`
	Target     string                     `json:"protected_target"`
	FieldProbe *ClientGameOverFieldProbe  `json:"field_probe,omitempty"`
	Call       *ClientGameOverProcessCall `json:"call,omitempty"`
	Behavior   string                     `json:"behavior"`
}

type ClientGameOverProcessTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	entry    uintptr
	target   uintptr
	original []byte
	patch    []byte
	page     uintptr
	record   uintptr
	probe    *ClientGameOverFieldProbe
}

// InstallClientGameOverProcessTracerWithFieldProbe additionally replaces the
// first result's four extension pairs for one handler invocation. Passing nil
// preserves the read-only tracer behavior.
func InstallClientGameOverProcessTracerWithFieldProbe(pid uint32, timeout time.Duration, probe *ClientGameOverFieldProbe) (*ClientGameOverProcessTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientGameOverProcessTracer{pid: pid, process: process, started: time.Now()}
	if probe != nil {
		probeCopy := *probe
		tracer.probe = &probeCopy
	}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	base, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	tracer.entry = base + clientGameOverProcessRVA
	tracer.target = base + clientGameOverProcessTargetRVA
	tracer.original, _ = readRemote(process, tracer.entry, len(clientGameOverProcessSignature))
	if !equalBytes(tracer.original, clientGameOverProcessSignature) {
		return nil, fmt.Errorf("Client GameOver process signature mismatch at 0x%08X: got %X", tracer.entry, tracer.original)
	}
	originalTarget := uintptr(int64(tracer.entry+5) + int64(int32(binary.LittleEndian.Uint32(tracer.original[1:5]))))
	if originalTarget != tracer.target {
		return nil, fmt.Errorf("Client GameOver process target 0x%08X, want 0x%08X", originalTarget, tracer.target)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx Client GameOver process trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + clientGameOverTraceRecordOffset
	stub := buildClientGameOverProcessTraceStub(tracer.record, tracer.target, tracer.probe)
	if clientGameOverTraceRecordOffset+clientGameOverTraceRecordSize > 0x1000 {
		return nil, fmt.Errorf("Client GameOver process trace record exceeds its allocation")
	}
	if len(stub) >= clientGameOverTraceRecordOffset {
		return nil, fmt.Errorf("Client GameOver process trace stub is unexpectedly large: %d", len(stub))
	}
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write Client GameOver process trace stub: %w", err)
	}
	tracer.patch = relativeJumpPatch(tracer.entry, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.entry, tracer.patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client GameOver process entry at 0x%08X", tracer.entry)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.entry, uintptr(len(tracer.patch)))
	failed = false
	return tracer, nil
}

func buildClientGameOverProcessTraceStub(record, target uintptr, probe *ClientGameOverFieldProbe) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // active++
	stub = append(stub, 0x83, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x00, 0x0F, 0x85, 0, 0, 0, 0) // cmp committed,0; jne finish
	alreadyCommittedJump := len(stub) - 4

	stub = append(stub, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // calls++
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // thread ID
	fields := []struct {
		stackOffset  byte
		recordOffset uintptr
	}{
		{0x24, 16}, // caller
		{0x18, 20}, // saved ECX / this
		{0x28, 24}, // schema
		{0x2C, 28}, // native object
		{0x30, 32}, // first stack DWORD after the two confirmed arguments
	}
	for _, field := range fields {
		stub = append(stub, 0x8B, 0x44, 0x24, field.stackOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+field.recordOffset))
	}
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36)) // original ESP
	stub = append(stub, 0x8B, 0x74, 0x24, 0x2C, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	nullObjectJump := len(stub) - 4
	stub = append(stub, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+0x40))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, clientGameOverNativeSize)
	stub = append(stub, 0xFC, 0xF3, 0xA4) // cld; rep movsb
	probeSkipJumps := make([]int, 0, 2)
	if probe != nil {
		// The snapshot above deliberately records the original decoded object.
		// Only then mutate the first result for a controlled UI differential.
		stub = append(stub,
			0x8B, 0x54, 0x24, 0x2C, // mov edx,[esp+2c] (native object)
			0x81, 0x7C, 0x24, 0x28, 0xBB, 0x0F, 0x00, 0x00, // cmp schema,0x0fbb
			0x0F, 0x85, 0, 0, 0, 0, // jne publish
		)
		probeSkipJumps = append(probeSkipJumps, len(stub)-4)
		if probe.ReplaceFields {
			stub = append(stub,
				0x80, 0x7A, 0x04, 0x00, // cmp byte ptr [edx+4],0 (ResultCount)
				0x0F, 0x84, 0, 0, 0, 0, // je publish
			)
			probeSkipJumps = append(probeSkipJumps, len(stub)-4)
			stub = append(stub, 0xC6, 0x42, 0x10, 0x04) // first result FieldCount=4
			for index, value := range probe.FieldValue1 {
				stub = append(stub, 0xC7, 0x42, byte(0x11+index*4))
				stub = binary.LittleEndian.AppendUint32(stub, value)
			}
			for index, value := range probe.FieldValue2 {
				stub = append(stub, 0xC7, 0x42, byte(0x21+index*4))
				stub = binary.LittleEndian.AppendUint32(stub, value)
			}
		}
		if probe.GameMode != nil {
			stub = append(stub, 0xC6, 0x82)
			stub = binary.LittleEndian.AppendUint32(stub, clientGameOverNativeSize-1)
			stub = append(stub, *probe.GameMode)
		}
	}
	publishOffset := len(stub)
	patchNearJump(stub, nullObjectJump, publishOffset)
	for _, displacement := range probeSkipJumps {
		patchNearJump(stub, displacement, publishOffset)
	}
	stub = append(stub, 0xC7, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x01, 0, 0, 0) // committed=1, after the complete copy

	finishOffset := len(stub)
	patchNearJump(stub, alreadyCommittedJump, finishOffset)
	stub = append(stub, 0xF0, 0xFF, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // active--
	stub = append(stub, 0x61, 0x9D, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(target))
	stub = append(stub, 0xC3) // push target; ret, preserving original registers
	return stub
}

func patchNearJump(stub []byte, displacementIndex, target int) {
	displacement := int64(target) - int64(displacementIndex+4)
	binary.LittleEndian.PutUint32(stub[displacementIndex:displacementIndex+4], uint32(int32(displacement)))
}

func (tracer *ClientGameOverProcessTracer) Capture(duration time.Duration) ClientGameOverProcessCapture {
	if duration <= 0 {
		duration = 20 * time.Minute
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		header, ok := readRemote(tracer.process, tracer.record, 8)
		if ok && binary.LittleEndian.Uint32(header[0:4]) != 0 {
			for attempt := 0; attempt < 100; attempt++ {
				active, activeOK := readRemote(tracer.process, tracer.record+4, 4)
				if !activeOK || binary.LittleEndian.Uint32(active) == 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			record, _ := readRemote(tracer.process, tracer.record, clientGameOverTraceRecordSize)
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture(nil)
}

func (tracer *ClientGameOverProcessTracer) capture(record []byte) ClientGameOverProcessCapture {
	result := ClientGameOverProcessCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Module: "Client.exe",
		Entry: fmt.Sprintf("0x%08X", tracer.entry), Target: fmt.Sprintf("0x%08X", tracer.target),
		Behavior: "one-shot decoded NOTIFY_GAME_OVER process-entry snapshot; records the 358-byte native object, preserves all registers and flags, then enters the original protected target; exact five-byte JMP restored on close",
	}
	if tracer.probe != nil {
		probeCopy := *tracer.probe
		result.FieldProbe = &probeCopy
		if tracer.probe.ReplaceFields {
			result.Behavior += "; after snapshotting, replaces only the first result's four extension pairs for a transient local display probe"
		}
		if tracer.probe.GameMode != nil {
			result.Behavior += fmt.Sprintf("; after snapshotting, replaces only GameMode with %d for a transient enum probe", *tracer.probe.GameMode)
		}
	}
	if len(record) != clientGameOverTraceRecordSize || binary.LittleEndian.Uint32(record[0:4]) == 0 {
		return result
	}
	native := record[0x40:]
	call := &ClientGameOverProcessCall{
		Calls: binary.LittleEndian.Uint32(record[8:12]), ThreadID: binary.LittleEndian.Uint32(record[12:16]),
		Caller: formatTracePointer(record[16:20]), This: formatTracePointer(record[20:24]),
		SchemaID: binary.LittleEndian.Uint32(record[24:28]), NativeObject: formatTracePointer(record[28:32]),
		StackAfterArguments: formatTracePointer(record[32:36]), StackPointer: formatTracePointer(record[36:40]),
		Time: binary.LittleEndian.Uint32(native[0:4]), ResultCount: native[4], GameMode: native[357],
		NativeHex: hex.EncodeToString(native),
	}
	call.SchemaHex = fmt.Sprintf("0x%08X", call.SchemaID)
	hash := sha256.Sum256(native)
	call.NativeSHA256 = hex.EncodeToString(hash[:])
	count := int(call.ResultCount)
	if count > 8 {
		count = 8
	}
	for index := 0; index < count; index++ {
		base := 5 + index*44
		fieldCount := native[base+11]
		boundedFieldCount := int(fieldCount)
		if boundedFieldCount > 4 {
			boundedFieldCount = 4
		}
		entry := ClientGameOverNativeResult{
			PlayerID: binary.LittleEndian.Uint16(native[base : base+2]), Result: native[base+2],
			Remark: binary.LittleEndian.Uint32(native[base+3 : base+7]),
			Point:  binary.LittleEndian.Uint32(native[base+7 : base+11]), FieldCount: fieldCount,
			FieldValue1: make([]uint32, 0, boundedFieldCount), FieldValue2: make([]uint32, 0, boundedFieldCount),
		}
		for field := 0; field < boundedFieldCount; field++ {
			entry.FieldValue1 = append(entry.FieldValue1, binary.LittleEndian.Uint32(native[base+12+field*4:base+16+field*4]))
			entry.FieldValue2 = append(entry.FieldValue2, binary.LittleEndian.Uint32(native[base+28+field*4:base+32+field*4]))
		}
		call.Results = append(call.Results, entry)
	}
	result.Call = call
	return result
}

func (tracer *ClientGameOverProcessTracer) Close() {
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
	if tracer.page != 0 {
		time.Sleep(20 * time.Millisecond)
		idle := false
		for attempt := 0; attempt < 100; attempt++ {
			active, ok := readRemote(tracer.process, tracer.record+4, 4)
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
