package winlaunch

import "testing"

func TestLoginCredentialCaptureStubRetainsVerifiedPrologue(t *testing.T) {
	stub := buildLoginCredentialCaptureStub(0x10000000, 0x10000200, 0x00400000)
	wantMinimum := len(clientNativeLoginSignature) + 5
	if len(stub) < wantMinimum {
		t.Fatalf("stub length = %d", len(stub))
	}
	prologueOffset := len(stub) - 5 - len(clientNativeLoginSignature)
	if !equalBytes(stub[prologueOffset:prologueOffset+len(clientNativeLoginSignature)], clientNativeLoginSignature) {
		t.Fatalf("stub does not replay native Login prologue: %X", stub)
	}
}
