package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	objectMethodResultMaximumSlots  = 220
	objectMethodResultArgumentCount = 9
	objectMethodResultPayloadSize   = 128
	objectMethodResultRecordOffset  = uintptr(0x2000)
	objectMethodResultPayloadOffset = 0x80
)

// ObjectMethodResultCapture records one COM-style vtable method after the
// original implementation has returned. It is deliberately separate from the
// normal entry-only object tracer: old QQTang interfaces often return decoded
// buffers through their final two pointer arguments, and observing those
// outputs is necessary to distinguish a transport failure from a later route
// rejection.
type ObjectMethodResultCapture struct {
	PID            uint32   `json:"pid"`
	ElapsedMS      int64    `json:"elapsed_ms"`
	Object         string   `json:"object"`
	OriginalVTable string   `json:"original_vtable"`
	CloneVTable    string   `json:"clone_vtable"`
	Slot           uint32   `json:"slot"`
	OriginalMethod string   `json:"original_method"`
	TotalCalls     uint32   `json:"total_calls"`
	CompletedCalls uint32   `json:"completed_calls"`
	ReturnValue    string   `json:"return_value,omitempty"`
	Arguments      []string `json:"stack_arguments,omitempty"`
	OutputPointer  string   `json:"output_pointer,omitempty"`
	OutputLength   uint32   `json:"output_length,omitempty"`
	CapturedBytes  uint32   `json:"captured_bytes,omitempty"`
	PayloadHex     string   `json:"payload_hex,omitempty"`
	Behavior       string   `json:"behavior"`
}

type ObjectMethodResultTracer struct {
	pid            uint32
	process        syscall.Handle
	started        time.Time
	object         uintptr
	originalObject []byte
	originalVTable uintptr
	cloneVTable    uintptr
	slot           uint32
	originalMethod uintptr
	page           uintptr
	record         uintptr
}

// InstallObjectMethodResultTracer clones one verified object's vtable and
// wraps only slot. The wrapper calls the exact original function, then records
// EAX plus the two output values addressed by stack arguments 7 and 8. This is
// the ABI used by QQTModules' packet decoder. No module code is patched.
func InstallObjectMethodResultTracer(pid uint32, object uintptr, slots int, slot, outputPointerArgument, outputLengthArgument uint32) (*ObjectMethodResultTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if object < 0x10000 || object > 0xffffffff {
		return nil, fmt.Errorf("object address is invalid: 0x%X", object)
	}
	if slots < 1 || slots > objectMethodResultMaximumSlots {
		return nil, fmt.Errorf("vtable slot count %d is outside 1..%d", slots, objectMethodResultMaximumSlots)
	}
	if slot >= uint32(slots) {
		return nil, fmt.Errorf("method slot %d is outside 0..%d", slot, slots-1)
	}
	if outputPointerArgument < 1 || outputPointerArgument > objectMethodResultArgumentCount ||
		outputLengthArgument < 1 || outputLengthArgument > objectMethodResultArgumentCount {
		return nil, fmt.Errorf("output arguments %d/%d are outside 1..%d", outputPointerArgument, outputLengthArgument, objectMethodResultArgumentCount)
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ObjectMethodResultTracer{pid: pid, process: process, started: time.Now(), object: object, slot: slot}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.originalObject, _ = readRemote(process, object, 4)
	if len(tracer.originalObject) != 4 {
		return nil, fmt.Errorf("read object 0x%08X", object)
	}
	tracer.originalVTable = uintptr(binary.LittleEndian.Uint32(tracer.originalObject))
	vtableBytes, ok := readRemote(process, tracer.originalVTable, slots*4)
	if !ok || len(vtableBytes) != slots*4 {
		return nil, fmt.Errorf("read %d object methods at 0x%08X", slots, tracer.originalVTable)
	}
	tracer.originalMethod = uintptr(binary.LittleEndian.Uint32(vtableBytes[int(slot)*4 : int(slot+1)*4]))
	if tracer.originalMethod < 0x10000 {
		return nil, fmt.Errorf("object method %d is invalid: 0x%08X", slot, tracer.originalMethod)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x3000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx object method result trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.cloneVTable = page + 0x1000
	tracer.record = page + objectMethodResultRecordOffset
	stub := buildObjectMethodResultTraceStub(page, tracer.record, tracer.originalMethod, outputPointerArgument-1, outputLengthArgument-1)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write object method result trace: %w", err)
	}
	clone := append([]byte(nil), vtableBytes...)
	binary.LittleEndian.PutUint32(clone[int(slot)*4:int(slot+1)*4], uint32(page))
	if err := writeRemote(process, tracer.cloneVTable, clone); err != nil {
		return nil, fmt.Errorf("write object method result vtable: %w", err)
	}
	clonePointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(clonePointer, uint32(tracer.cloneVTable))
	if !ensureRemoteBytes(process, object, clonePointer, pageReadWrite) {
		return nil, fmt.Errorf("patch object 0x%08X", object)
	}
	procFlushInstruction.Call(uintptr(process), page, 0x3000)
	failed = false
	return tracer, nil
}

func buildObjectMethodResultTraceStub(stubAddress, recordAddress, originalMethod uintptr, outputPointerArgument, outputLengthArgument uint32) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	// Count entries and retain the exact register/argument frame before calling
	// the original stdcall implementation.
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress))
	stub = append(stub, 0x8B, 0x54, 0x24, 0x18, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+8)) // saved ECX
	for index := 0; index < objectMethodResultArgumentCount; index++ {
		stub = append(stub, 0x8B, 0x54, 0x24, byte(0x28+index*4), 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+12+uintptr(index*4)))
	}
	// Replace the original return address with our post-call continuation. The
	// original method remains responsible for its own stdcall argument cleanup.
	stub = append(stub, 0x8B, 0x54, 0x24, 0x24, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+4))
	stub = append(stub, 0xBA)
	afterImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	stub = append(stub, 0x89, 0x54, 0x24, 0x24, 0x61, 0x9D, 0xE9)
	originalJump := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	afterOffset := len(stub)
	afterAddress := stubAddress + uintptr(afterOffset)
	binary.LittleEndian.PutUint32(stub[afterImmediate:], uint32(afterAddress))
	originalNext := stubAddress + uintptr(originalJump+4)
	binary.LittleEndian.PutUint32(stub[originalJump:], uint32(originalMethod-originalNext))

	stub = append(stub, 0x9C, 0x60) // preserve the exact original return state
	stub = append(stub, 0x8B, 0x54, 0x24, 0x1C, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+48)) // returned EAX
	// The selected stack arguments point to the returned buffer pointer and its
	// byte length. Both addresses were saved before the original call.
	stub = append(stub, 0x8B, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+12+uintptr(outputPointerArgument*4)))
	stub = append(stub, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingPointer := len(stub) - 4
	stub = append(stub, 0x8B, 0x16, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+52))
	stub = append(stub, 0x8B, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+12+uintptr(outputLengthArgument*4)))
	stub = append(stub, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingLength := len(stub) - 4
	stub = append(stub, 0x8B, 0x0E, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+56))
	stub = append(stub, 0x8B, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+52))
	stub = append(stub, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingBuffer := len(stub) - 4
	stub = append(stub, 0x85, 0xC9, 0x0F, 0x84, 0, 0, 0, 0)
	missingBytes := len(stub) - 4
	stub = append(stub, 0x81, 0xF9)
	stub = binary.LittleEndian.AppendUint32(stub, objectMethodResultPayloadSize)
	stub = append(stub, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOK := len(stub) - 4
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, objectMethodResultPayloadSize)
	copyStart := len(stub)
	patchRelative32(stub, lengthOK, copyStart)
	stub = append(stub, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+60))
	stub = append(stub, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+objectMethodResultPayloadOffset))
	stub = append(stub, 0xF3, 0xA4) // rep movsb
	doneCopy := len(stub)
	for _, displacement := range []int{missingPointer, missingLength, missingBuffer, missingBytes} {
		patchRelative32(stub, displacement, doneCopy)
	}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+64)) // completed calls
	stub = append(stub, 0x61, 0x9D, 0xFF, 0x25)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+4))
	return stub
}

func (tracer *ObjectMethodResultTracer) Capture(duration time.Duration) ObjectMethodResultCapture {
	if duration <= 0 {
		duration = 10 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if record, ok := readRemote(tracer.process, tracer.record+64, 4); ok && len(record) == 4 && binary.LittleEndian.Uint32(record) != 0 {
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *ObjectMethodResultTracer) capture() ObjectMethodResultCapture {
	result := ObjectMethodResultCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Object: fmt.Sprintf("0x%08X", tracer.object),
		OriginalVTable: fmt.Sprintf("0x%08X", tracer.originalVTable), CloneVTable: fmt.Sprintf("0x%08X", tracer.cloneVTable),
		Slot: tracer.slot, OriginalMethod: fmt.Sprintf("0x%08X", tracer.originalMethod),
		Behavior: "transparent post-return vtable-method trace; exact original method called; module code unchanged",
	}
	record, ok := readRemote(tracer.process, tracer.record, objectMethodResultPayloadOffset+objectMethodResultPayloadSize)
	if !ok || len(record) < objectMethodResultPayloadOffset {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(record[0:4])
	result.CompletedCalls = binary.LittleEndian.Uint32(record[64:68])
	if result.TotalCalls == 0 {
		return result
	}
	result.ReturnValue = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[48:52]))
	for index := 0; index < objectMethodResultArgumentCount; index++ {
		result.Arguments = append(result.Arguments, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12+index*4:16+index*4])))
	}
	result.OutputPointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[52:56]))
	result.OutputLength = binary.LittleEndian.Uint32(record[56:60])
	result.CapturedBytes = binary.LittleEndian.Uint32(record[60:64])
	if result.CapturedBytes > objectMethodResultPayloadSize {
		result.CapturedBytes = objectMethodResultPayloadSize
	}
	result.PayloadHex = hex.EncodeToString(record[objectMethodResultPayloadOffset : objectMethodResultPayloadOffset+int(result.CapturedBytes)])
	return result
}

func (tracer *ObjectMethodResultTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.object != 0 && len(tracer.originalObject) == 4 {
		current, ok := readRemote(tracer.process, tracer.object, 4)
		if ok && len(current) == 4 && uintptr(binary.LittleEndian.Uint32(current)) == tracer.cloneVTable {
			ensureRemoteBytes(tracer.process, tracer.object, tracer.originalObject, pageReadWrite)
		}
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
