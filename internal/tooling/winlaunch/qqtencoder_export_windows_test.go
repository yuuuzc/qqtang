package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildQQTEncoderExportStubArgumentOrder(t *testing.T) {
	stub := buildQQTEncoderExportStub(
		0x11111111, 0x22222222, 0x816, 0x33333333, 0x44444444,
		0x55555555, 0x66666666,
	)
	if len(stub) != 44 {
		t.Fatalf("stub length = %d, want 44", len(stub))
	}
	if stub[0] != 0x6a || stub[1] != 0x01 {
		t.Fatalf("version push = %x", stub[:2])
	}
	wantPushes := []uint32{0x33333333, 0x55555555, 0x44444444, 0x816, 0x11111111}
	for index, want := range wantPushes {
		offset := 2 + index*5
		if stub[offset] != 0x68 {
			t.Fatalf("push %d opcode = %02x", index, stub[offset])
		}
		if got := binary.LittleEndian.Uint32(stub[offset+1:]); got != want {
			t.Fatalf("push %d = 0x%08x, want 0x%08x", index, got, want)
		}
	}
	if got := binary.LittleEndian.Uint32(stub[28:]); got != 0x22222222 {
		t.Fatalf("method = 0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(stub[35:]); got != 0x66666666 {
		t.Fatalf("return address = 0x%08x", got)
	}
}
