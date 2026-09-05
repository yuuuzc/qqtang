package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildNetCenterOnReceiveTraceStub(t *testing.T) {
	const (
		record = uintptr(0x12345000)
		stub   = uintptr(0x20000000)
		target = uintptr(0x08B710C3)
	)
	code := buildNetCenterOnReceiveTraceStub(record)
	finalizeNetCenterOnReceiveTraceStub(code, stub, target)
	if len(code) < 8 || code[0] != 0x9C || code[1] != 0x60 || code[len(code)-5] != 0xE9 {
		t.Fatalf("unexpected stub shape: %X", code)
	}
	displacement := int32(binary.LittleEndian.Uint32(code[len(code)-4:]))
	jumpTarget := uintptr(int64(stub+uintptr(len(code))) + int64(displacement))
	if jumpTarget != target {
		t.Fatalf("jump target 0x%X, want 0x%X", jumpTarget, target)
	}
}
