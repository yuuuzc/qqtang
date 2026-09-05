package winlaunch

import (
	"bytes"
	"testing"
)

func TestBuildClientGameEventCallbackTraceStub(t *testing.T) {
	stub := buildClientGameEventCallbackTraceStub(
		0x10000000, 0x10000500, 0x005A23C4, clientGameEventCallbackSignature,
	)
	if !bytes.Contains(stub, clientGameEventCallbackSignature) {
		t.Fatalf("callback stub does not replay exact prologue: %x", stub)
	}
	if len(stub) < 5 || stub[len(stub)-5] != 0xE9 {
		t.Fatalf("callback stub has no tail jump: %x", stub)
	}
}

func TestBuildClientGameOverConsumeTraceStub(t *testing.T) {
	stub := buildClientGameOverConsumeTraceStub(
		0x10000280, 0x10000500, 0x005A3B68, clientGameOverConsumeSignature,
	)
	if !bytes.Contains(stub, clientGameOverConsumeSignature) {
		t.Fatalf("consumer stub does not replay exact prologue: %x", stub)
	}
	if len(stub) < 5 || stub[len(stub)-5] != 0xE9 {
		t.Fatalf("consumer stub has no tail jump: %x", stub)
	}
}

func TestRelativeJumpPatchPreservesRequestedWidth(t *testing.T) {
	patch := relativeJumpPatch(0x005A23BC, 0x10000000, len(clientGameEventCallbackSignature))
	if len(patch) != len(clientGameEventCallbackSignature) || patch[0] != 0xE9 {
		t.Fatalf("relative patch = %x", patch)
	}
	for _, value := range patch[5:] {
		if value != 0x90 {
			t.Fatalf("relative patch padding = %x", patch[5:])
		}
	}
}
