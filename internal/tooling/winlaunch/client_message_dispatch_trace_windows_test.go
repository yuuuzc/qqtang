package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildClientMessageDispatchTraceStub(t *testing.T) {
	const (
		stubAddress = uintptr(0x10000000)
		lookup      = uintptr(0x005DD98F)
	)
	stub := buildClientMessageDispatchTraceStub(stubAddress, 0x10000800, 0x10000804, 0x10001000, lookup)
	if !bytes.HasPrefix(stub, []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x04, 0x53, 0x56, 0x57}) {
		t.Fatalf("unexpected dispatcher stub prologue: %X", stub[:9])
	}
	callMarker := []byte{0xFF, 0x75, 0x08, 0x8B, 0xCE, 0xE8}
	callAt := bytes.Index(stub, callMarker)
	if callAt < 0 {
		t.Fatalf("dispatcher stub has no handler-lookup call: %X", stub)
	}
	displacementAt := callAt + len(callMarker)
	callNext := stubAddress + uintptr(displacementAt+4)
	target := uintptr(int64(callNext) + int64(int32(binary.LittleEndian.Uint32(stub[displacementAt:displacementAt+4]))))
	if target != lookup {
		t.Fatalf("lookup target = 0x%08X, want 0x%08X", target, lookup)
	}
	if !bytes.HasSuffix(stub, []byte{0x5F, 0x5E, 0x5B, 0x8B, 0xE5, 0x5D, 0xC2, 0x04, 0x00}) {
		t.Fatalf("dispatcher stub does not restore nonvolatile registers and ret 4: %X", stub[len(stub)-9:])
	}
	if bytes.Contains(stub, []byte{0xF3, 0xA5}) {
		t.Fatal("dispatcher stub must not dereference the opaque caller argument with rep movsd")
	}
	if !bytes.Contains(stub, []byte{0x8B, 0x41, 0x04, 0x89, 0x42, 0x28}) {
		t.Fatal("dispatcher stub must record handler vtable+4 as the processing method")
	}
	if !bytes.Contains(stub, []byte{0x8B, 0x49, 0x08, 0x89, 0x4A, 0x18}) {
		t.Fatal("dispatcher stub must record handler vtable+8 as the acceptance method")
	}
	if !bytes.Contains(stub, []byte{0x8B, 0x13, 0x8B, 0xCB, 0xFF, 0x52, 0x08}) {
		t.Fatal("dispatcher stub must preserve the original vtable+8 acceptance call")
	}
	if required := clientMessageDispatchRecordsStart + clientMessageDispatchCapacity*clientMessageDispatchRecordSize; required > clientMessageDispatchAllocation {
		t.Fatalf("trace allocation 0x%X is smaller than required 0x%X", clientMessageDispatchAllocation, required)
	}
}
