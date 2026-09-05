package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildClientJoinTraceStubReplaysFullPrologue(t *testing.T) {
	page := uintptr(0x12000000)
	record := page + 0x300
	resume := uintptr(0x0052D579)
	serviceLocator := uintptr(0x007FAF04)
	stub := buildClientJoinTraceStub(page, record, resume, serviceLocator, clientJoinRequestSignature)
	if len(stub) < len(clientJoinRequestSignature)+5 {
		t.Fatalf("stub too short: %d", len(stub))
	}
	replayed := stub[len(stub)-5-len(clientJoinRequestSignature) : len(stub)-5]
	if !bytes.Equal(replayed, clientJoinRequestSignature) {
		t.Fatalf("replayed prologue=%X", replayed)
	}
	if got := binary.LittleEndian.Uint32(stub[len(stub)-4:]); got == 0 {
		t.Fatal("expected nonzero resume displacement")
	}
}

func TestBuildClientJoinStateTraceStubReplaysCondition(t *testing.T) {
	page := uintptr(0x13000000)
	stub := buildClientJoinStateTraceStub(page, page+0x300, 0x0052D648, clientJoinStateSignature)
	replayed := stub[len(stub)-5-len(clientJoinStateSignature) : len(stub)-5]
	if !bytes.Equal(replayed, clientJoinStateSignature) {
		t.Fatalf("replayed condition=%X", replayed)
	}
}

func TestBuildClientJoinOutputTraceStubReplaysSelectionLoad(t *testing.T) {
	page := uintptr(0x14000000)
	stub := buildClientJoinOutputTraceStub(page, page+0x300, 0x0052D675, clientJoinOutputSignature)
	replayed := stub[len(stub)-5-len(clientJoinOutputSignature) : len(stub)-5]
	if !bytes.Equal(replayed, clientJoinOutputSignature) {
		t.Fatalf("replayed selection load=%X", replayed)
	}
	if !bytes.Contains(stub, []byte{0xFC, 0xF3, 0xA5}) {
		t.Fatal("expected forward frame copy")
	}
}

func TestBuildQQTModulesStageCNetworkTraceStubReplaysGate(t *testing.T) {
	page := uintptr(0x15000000)
	stub := buildQQTModulesStageCNetworkTraceStub(page, page+0x300, 0x06B1942C, 0x06B8F744, qqtModulesStageCNetworkSignature)
	replayed := stub[len(stub)-5-len(qqtModulesStageCNetworkSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtModulesStageCNetworkSignature) {
		t.Fatalf("replayed interface gate=%X", replayed)
	}
}

func TestBuildQQTSectionServicesTraceStubReplaysArgumentLoad(t *testing.T) {
	page := uintptr(0x16000000)
	stub := buildQQTSectionServicesTraceStub(page, page+0x300, 0x01989923, qqtSectionServicesSignature)
	replayed := stub[len(stub)-5-len(qqtSectionServicesSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtSectionServicesSignature) {
		t.Fatalf("replayed QQTSection argument load=%X", replayed)
	}
}

func TestBuildQQTSectionReloadGateStubReplaysQQTModulesGate(t *testing.T) {
	page := uintptr(0x17000000)
	stub := buildQQTSectionReloadGateStub(page, page+0x300, 0x06B1942C, qqtModulesStageCNetworkSignature)
	replayed := stub[len(stub)-5-len(qqtModulesStageCNetworkSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtModulesStageCNetworkSignature) {
		t.Fatalf("replayed reload gate=%X", replayed)
	}
	if !bytes.Contains(stub, []byte{0xF3, 0x90, 0x83, 0x3D}) {
		t.Fatal("expected PAUSE-based release gate")
	}
}

func TestBuildQQTSectionSendTraceStubReplaysDispatchSetup(t *testing.T) {
	page := uintptr(0x18000000)
	stub := buildQQTSectionSendTraceStub(page, page+0x300, 0x01989C92, qqtSectionSendSignature)
	replayed := stub[len(stub)-5-len(qqtSectionSendSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtSectionSendSignature) {
		t.Fatalf("replayed QQTSection send setup=%X", replayed)
	}
	if !bytes.Contains(stub, []byte{0xFC, 0xF3, 0xA5, 0xA4}) {
		t.Fatal("expected exact login-packet copy")
	}
}

func TestBuildQQTSectionTransportTraceStubReplaysServiceLoads(t *testing.T) {
	page := uintptr(0x19000000)
	stub := buildQQTSectionTransportTraceStub(page, page+0x300, 0x0198AF78, qqtSectionTransportSignature)
	replayed := stub[len(stub)-5-len(qqtSectionTransportSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtSectionTransportSignature) {
		t.Fatalf("replayed QQTSection transport service loads=%X", replayed)
	}
}

func TestBuildQQTSectionPreconnectStubCreatesAndBindsConnection(t *testing.T) {
	page := uintptr(0x1A000000)
	record := page + 0x300
	stub := buildQQTSectionPreconnectStub(page, record, 0x0198AF78, qqtSectionTransportSignature, 1, 0x0100007F, 18000)
	replayed := stub[len(stub)-5-len(qqtSectionTransportSignature) : len(stub)-5]
	if !bytes.Equal(replayed, qqtSectionTransportSignature) {
		t.Fatalf("replayed QQTSection transport loads=%X", replayed)
	}
	if !bytes.Contains(stub, []byte{0xFF, 0x90, 0x58, 0x01, 0x00, 0x00}) {
		t.Fatal("expected NetCenter transport vtable +0x158 create call")
	}
	if !bytes.Contains(stub, []byte{0x66, 0xC7, 0x42, 0x10, 0x01, 0x00}) {
		t.Fatal("expected route-ID bind on the new connection object")
	}
	completion := []byte{0xC7, 0x05}
	completion = binary.LittleEndian.AppendUint32(completion, uint32(record+48))
	completion = binary.LittleEndian.AppendUint32(completion, 1)
	if !bytes.Contains(stub, completion) {
		t.Fatal("expected completion marker after the create and bind sequence")
	}
}
