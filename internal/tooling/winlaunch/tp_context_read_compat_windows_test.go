package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildTPContextReadBootstrapFixups(t *testing.T) {
	stub := buildTPContextReadBootstrap(0x76543210, 0x02000000, 0x02000824, 0x70123456, 0x70876543)
	for _, value := range []uint32{0x76543210, 0x02000000, 0x02000824, 0x70123456, 0x70876543} {
		needle := make([]byte, 4)
		binary.LittleEndian.PutUint32(needle, value)
		if !bytes.Contains(stub, needle) {
			t.Fatalf("bootstrap does not contain fixup 0x%08X: %X", value, stub)
		}
	}
	if !bytes.Contains(stub, []byte{0x8B, 0x0A}) || stub[len(stub)-1] != 0xC3 {
		t.Fatalf("bootstrap does not replay the original boundary: %X", stub)
	}
	if got := binary.LittleEndian.Uint32(stub[4:8]); got != 0x02000824 {
		t.Fatalf("bootstrap handle predicate fixup = 0x%08X", got)
	}
	if got := binary.LittleEndian.Uint32(stub[19:23]); got != 0x76543210 {
		t.Fatalf("bootstrap add-handler fixup = 0x%08X", got)
	}
}

func TestBuildTPContextReadVEHResolves(t *testing.T) {
	handler, err := buildTPContextReadVEH(
		1234, 0x707B47FF, 0x707D5C73, tpETWRepairModule{base: 0x70050000, end: 0x70B70000},
		0x02000800, 0x03000000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(handler) == 0 || len(handler) >= int(tpContextStubOffset) {
		t.Fatalf("handler size = %d", len(handler))
	}
	if !bytes.Contains(handler, []byte{0x05, 0x00, 0x00, 0xC0}) {
		t.Fatalf("handler lacks STATUS_ACCESS_VIOLATION predicate")
	}
}

func TestBuildTPEarlyScanBootstrapSkipsSystemWriteAndKeepsStackTransition(t *testing.T) {
	stub := buildTPEarlyScanBootstrap(0x76543210, 0x02000000, 0x02000824, 0x707D4401)
	if bytes.Contains(stub, []byte{0x89, 0x02}) {
		t.Fatalf("early bootstrap still writes through legacy system pointer: %X", stub)
	}
	if !bytes.Contains(stub, []byte{0x81, 0xEC, 0xFC, 0xFF, 0xFF, 0xFF}) {
		t.Fatalf("early bootstrap lost verified stack transition: %X", stub)
	}
	if got := binary.LittleEndian.Uint32(stub[len(stub)-5 : len(stub)-1]); got != 0x707D4401 {
		t.Fatalf("early bootstrap continuation = 0x%08X", got)
	}
}
