package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildTPFreeStageTransitionGuardStubScopesTheSuppression(t *testing.T) {
	const (
		coreWrapperReturn    = uintptr(0x01E02622)
		coreTransitionReturn = uintptr(0x01E0F83B)
		forwardTarget        = uintptr(0x769F6040)
		counterAddress       = uintptr(0x016B0100)
	)
	stub := buildTPFreeStageTransitionGuardStub(coreWrapperReturn, coreTransitionReturn, forwardTarget, counterAddress)
	if len(stub) != 42 {
		t.Fatalf("stub length = %d, want 42", len(stub))
	}
	if got := binary.LittleEndian.Uint32(stub[3:7]); got != uint32(coreWrapperReturn) {
		t.Fatalf("Core wrapper return = 0x%08X", got)
	}
	if got := binary.LittleEndian.Uint32(stub[16:20]); got != uint32(coreTransitionReturn) {
		t.Fatalf("Core transition return = 0x%08X", got)
	}
	if got := binary.LittleEndian.Uint32(stub[28:32]); got != uint32(counterAddress) {
		t.Fatalf("counter address = 0x%08X", got)
	}
	if got := binary.LittleEndian.Uint32(stub[36:40]); got != uint32(forwardTarget) {
		t.Fatalf("forward target = 0x%08X", got)
	}
	if stub[32] != 0xC2 || stub[33] != 0x04 || stub[34] != 0x00 {
		t.Fatalf("matching path does not return with ret 4: %X", stub[32:35])
	}
	for _, displacementOffset := range []int{9, 22} {
		displacement := int32(binary.LittleEndian.Uint32(stub[displacementOffset : displacementOffset+4]))
		if target := displacementOffset + 4 + int(displacement); target != 35 {
			t.Fatalf("forward jump at %d targets %d, want 35", displacementOffset, target)
		}
	}
}

func TestParseCorePostQuitMessageIATCall(t *testing.T) {
	call := []byte{0xFF, 0x15, 0x88, 0xC7, 0x00, 0x10}
	address, ok := parseX86AbsoluteIndirectCall(call)
	if !ok || address != 0x1000C788 {
		t.Fatalf("Core PostQuitMessage IAT = 0x%X, ok=%t", address, ok)
	}
	for _, invalid := range [][]byte{
		{0xFF, 0x25, 0x88, 0xC7, 0x00, 0x10},
		{0x8B, 0xFF, 0x55, 0x8B, 0xEC, 0x6A},
		{0xFF, 0x15, 0, 0, 0, 0},
	} {
		if _, accepted := parseX86AbsoluteIndirectCall(invalid); accepted {
			t.Fatalf("invalid Core call was accepted: %X", invalid)
		}
	}
}
