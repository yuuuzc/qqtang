package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildNetCenterUDPCallbackTraceStub(t *testing.T) {
	const (
		stub    = uintptr(0x20000000)
		counter = uintptr(0x20000800)
		records = uintptr(0x20001000)
		resume  = uintptr(0x08726D79)
	)
	code := buildNetCenterUDPCallbackTraceStub(stub, counter, records, resume)
	if len(code) < len(netCenterUDPCallbackSignature)+7 || code[0] != 0x9C || code[1] != 0x60 {
		t.Fatalf("unexpected trace stub shape: %X", code)
	}
	signatureAt := len(code) - 5 - len(netCenterUDPCallbackSignature)
	if !bytes.Equal(code[signatureAt:signatureAt+len(netCenterUDPCallbackSignature)], netCenterUDPCallbackSignature) {
		t.Fatalf("original dispatch not reproduced: %X", code[signatureAt:])
	}
	if code[len(code)-5] != 0xE9 {
		t.Fatalf("missing final jump: %X", code[len(code)-8:])
	}
	displacement := int32(binary.LittleEndian.Uint32(code[len(code)-4:]))
	target := uintptr(int64(stub+uintptr(len(code))) + int64(displacement))
	if target != resume {
		t.Fatalf("resume target 0x%X, want 0x%X", target, resume)
	}
}
