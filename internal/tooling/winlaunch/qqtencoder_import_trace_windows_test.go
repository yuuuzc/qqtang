package winlaunch

import "testing"

func TestBuildQQTEncoderImportTraceStub(t *testing.T) {
	stub := buildQQTEncoderImportTraceStub(0x12345678, 0x23456789)
	if len(stub) < 80 {
		t.Fatalf("stub too short: %d", len(stub))
	}
	if stub[len(stub)-5] != 0xE9 {
		t.Fatalf("missing tail jump: %x", stub[len(stub)-5:])
	}
	finalizeQQTEncoderImportTraceStub(stub, 0x10000000, 0x20000000)
	if stub[len(stub)-4] == 0 && stub[len(stub)-3] == 0 && stub[len(stub)-2] == 0 && stub[len(stub)-1] == 0 {
		t.Fatal("tail jump was not finalized")
	}
}
