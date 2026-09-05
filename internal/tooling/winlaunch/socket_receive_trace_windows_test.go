package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildSocketReceiveTraceStub(t *testing.T) {
	const (
		stub       = uintptr(0x20000000)
		trampoline = uintptr(0x20000300)
		counter    = uintptr(0x20000800)
		records    = uintptr(0x20001000)
	)
	code := buildSocketReceiveTraceStub(stub, trampoline, counter, records)
	if len(code) < 32 || code[0] != 0x55 || code[1] != 0x8B || code[2] != 0xEC {
		t.Fatalf("unexpected recvfrom stub shape: %X", code)
	}
	callOffset := 3 + 6*3
	if code[callOffset] != 0xE8 {
		t.Fatalf("trampoline call missing at %d: %X", callOffset, code)
	}
	displacement := int32(binary.LittleEndian.Uint32(code[callOffset+1 : callOffset+5]))
	target := uintptr(int64(stub+uintptr(callOffset+5)) + int64(displacement))
	if target != trampoline {
		t.Fatalf("trampoline target 0x%X, want 0x%X", target, trampoline)
	}
	wantTail := []byte{0x61, 0x9D, 0x8B, 0xE5, 0x5D, 0xC2, 0x18, 0}
	for index := range wantTail {
		if code[len(code)-len(wantTail)+index] != wantTail[index] {
			t.Fatalf("unexpected recvfrom return tail: %X", code[len(code)-len(wantTail):])
		}
	}
}
