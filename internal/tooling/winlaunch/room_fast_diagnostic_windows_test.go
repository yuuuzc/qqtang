package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildRoomFastReceiveHijackStubPreservesThreadContext(t *testing.T) {
	stub := buildRoomFastReceiveHijackStub(0x11111111, 0x22222222, 1, 1000001, 0x33333333, 40, 0x44444444, 0x55555555, 0x66666666)
	if !bytes.Equal(stub[:2], []byte{0x9C, 0x60}) {
		t.Fatalf("stub does not begin with pushfd/pushad: %X", stub[:2])
	}
	if !bytes.Contains(stub, []byte{0x61, 0x9D, 0x9C, 0x50, 0xB8}) {
		t.Fatalf("stub does not restore registers/flags before marking completion: %X", stub)
	}
	if stub[len(stub)-1] != 0xC3 || binary.LittleEndian.Uint32(stub[len(stub)-5:len(stub)-1]) != 0x66666666 {
		t.Fatalf("stub does not return to original EIP: %X", stub[len(stub)-6:])
	}
}
