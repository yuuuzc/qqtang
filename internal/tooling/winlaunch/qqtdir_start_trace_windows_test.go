package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildQQTDirStartTraceStubPreservesOriginalRegisters(t *testing.T) {
	const (
		page   = uintptr(0x02000000)
		record = uintptr(0x02000200)
		resume = uintptr(0x06C3922E)
	)
	original := []byte{0x8B, 0x08, 0x50, 0xFF, 0x51, 0x14}
	stub := buildQQTDirStartTraceStub(page, record, resume, original)

	wantPrefix := []byte{
		0x9C, 0x60,
		0xF0, 0xFF, 0x05, 0x00, 0x02, 0x00, 0x02,
		0xA3, 0x08, 0x02, 0x00, 0x02,
		0x8B, 0x54, 0x24, 0x24, 0x89, 0x15, 0x0C, 0x02, 0x00, 0x02,
		0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3, 0x04, 0x02, 0x00, 0x02,
		0x61, 0x9D,
	}
	if len(stub) < len(wantPrefix)+len(original)+5 {
		t.Fatalf("stub too short: %d", len(stub))
	}
	for index := range wantPrefix {
		if stub[index] != wantPrefix[index] {
			t.Fatalf("prefix byte %d = %02X, want %02X", index, stub[index], wantPrefix[index])
		}
	}
	originalOffset := len(wantPrefix)
	for index := range original {
		if stub[originalOffset+index] != original[index] {
			t.Fatalf("original byte %d not replayed", index)
		}
	}
	jumpOffset := originalOffset + len(original)
	if stub[jumpOffset] != 0xE9 {
		t.Fatalf("resume opcode = %02X, want E9", stub[jumpOffset])
	}
	displacement := int32(binary.LittleEndian.Uint32(stub[jumpOffset+1 : jumpOffset+5]))
	target := uintptr(int64(page+uintptr(jumpOffset)+5) + int64(displacement))
	if target != resume {
		t.Fatalf("resume target = 0x%08X, want 0x%08X", target, resume)
	}
}
