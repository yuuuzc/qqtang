package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildQQTEncoderImportStubArguments(t *testing.T) {
	stub := buildQQTEncoderImportStub(0x11111111, 0x22222222, 0x7d5, 0x33333333, 0x44444444, 0x2510, 0x55555555)
	wantPushes := []uint32{1, 0x2510, 0x44444444, 0x33333333, 0x7d5, 0x11111111}
	offset := 0
	if stub[offset] != 0x6a || stub[offset+1] != 1 {
		t.Fatalf("first push = %x", stub[:2])
	}
	offset += 2
	for index, want := range wantPushes[1:] {
		if stub[offset] != 0x68 {
			t.Fatalf("push %d opcode = 0x%02X", index+1, stub[offset])
		}
		if got := binary.LittleEndian.Uint32(stub[offset+1 : offset+5]); got != want {
			t.Fatalf("push %d value = 0x%08X, want 0x%08X", index+1, got, want)
		}
		offset += 5
	}
}
