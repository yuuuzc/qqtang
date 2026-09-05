package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildNetCenterDirectoryResponseTraceStub(t *testing.T) {
	const (
		page   = uintptr(0x20000000)
		record = uintptr(0x20000300)
		resume = uintptr(0x08B74664)
	)
	stub := buildNetCenterDirectoryResponseTraceStub(page, record, resume)
	if len(stub) < 8 || stub[0] != 0x9C || stub[1] != 0x60 || stub[len(stub)-5] != 0xE9 {
		t.Fatalf("unexpected stub shape: %X", stub)
	}
	displacement := int32(binary.LittleEndian.Uint32(stub[len(stub)-4:]))
	jumpTarget := uintptr(int64(page+uintptr(len(stub))) + int64(displacement))
	if jumpTarget != resume {
		t.Fatalf("jump target 0x%X, want 0x%X", jumpTarget, resume)
	}
}
