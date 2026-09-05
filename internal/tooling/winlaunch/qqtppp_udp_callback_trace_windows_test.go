package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildQQTPPPUDPCallbackTraceStub(t *testing.T) {
	const (
		stub    = uintptr(0x23000000)
		counter = uintptr(0x23000800)
		records = uintptr(0x23001000)
		resume  = uintptr(0x019D7881)
	)
	code := buildQQTPPPUDPCallbackTraceStub(stub, counter, records, resume)
	if len(code) < len(qqtPPPUDPCallbackSignature)+7 || code[0] != 0x9C || code[1] != 0x60 {
		t.Fatalf("unexpected trace stub shape: %X", code)
	}
	signatureAt := len(code) - 5 - len(qqtPPPUDPCallbackSignature)
	if !bytes.Equal(code[signatureAt:signatureAt+len(qqtPPPUDPCallbackSignature)], qqtPPPUDPCallbackSignature) {
		t.Fatalf("original callback dispatch not reproduced: %X", code[signatureAt:])
	}
	displacement := int32(binary.LittleEndian.Uint32(code[len(code)-4:]))
	target := uintptr(int64(stub+uintptr(len(code))) + int64(displacement))
	if target != resume {
		t.Fatalf("resume target 0x%X, want 0x%X", target, resume)
	}
}
