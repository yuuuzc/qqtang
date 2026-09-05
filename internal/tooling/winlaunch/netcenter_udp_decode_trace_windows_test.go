package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildNetCenterUDPDecodeTraceStub(t *testing.T) {
	const (
		stub    = uintptr(0x22000000)
		counter = uintptr(0x22000800)
		records = uintptr(0x22001000)
		resume  = uintptr(0x087204AB)
	)
	code := buildNetCenterUDPDecodeTraceStub(stub, counter, records, resume)
	if len(code) < len(netCenterUDPDecodeSignature)+7 || code[0] != 0x9C || code[1] != 0x60 {
		t.Fatalf("unexpected trace stub shape: %X", code)
	}
	signatureAt := len(code) - 5 - len(netCenterUDPDecodeSignature)
	if !bytes.Equal(code[signatureAt:signatureAt+len(netCenterUDPDecodeSignature)], netCenterUDPDecodeSignature) {
		t.Fatalf("original decoder epilogue not reproduced: %X", code[signatureAt:])
	}
	displacement := int32(binary.LittleEndian.Uint32(code[len(code)-4:]))
	target := uintptr(int64(stub+uintptr(len(code))) + int64(displacement))
	if target != resume {
		t.Fatalf("resume target 0x%X, want 0x%X", target, resume)
	}
}
