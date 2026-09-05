package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestObjectMethodResultTraceStubPreservesOriginalDispatch(t *testing.T) {
	const (
		stubAddress    = uintptr(0x23000000)
		recordAddress  = uintptr(0x23002000)
		originalMethod = uintptr(0x02737F51)
	)
	stub := buildObjectMethodResultTraceStub(stubAddress, recordAddress, originalMethod, 7, 8)
	if len(stub) >= 0x1000 {
		t.Fatalf("stub length = %d", len(stub))
	}
	if stub[0] != 0x9C || stub[1] != 0x60 {
		t.Fatalf("stub does not preserve flags/registers: %X", stub[:8])
	}
	foundOriginal := false
	for index := 0; index+5 <= len(stub); index++ {
		if stub[index] != 0xE9 {
			continue
		}
		target := uintptr(int64(stubAddress+uintptr(index+5)) + int64(int32(binary.LittleEndian.Uint32(stub[index+1:index+5]))))
		if target == originalMethod {
			foundOriginal = true
			break
		}
	}
	if !foundOriginal {
		t.Fatalf("stub does not dispatch to original method 0x%08X: %X", originalMethod, stub)
	}
}
