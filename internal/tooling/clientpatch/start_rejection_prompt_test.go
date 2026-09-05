package clientpatch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestStartRejectionPromptCavePreservesOrdinaryRoomChat(t *testing.T) {
	cave := buildStartRejectionPromptCave()
	if !bytes.Contains(cave, []byte{0x66, 0x83, 0x3B, 0xFF}) {
		t.Fatalf("system SourcePlayerID comparison is missing: %X", cave)
	}
	ordinary := bytes.Index(cave, startRejectionPromptHookOriginal)
	if ordinary < 0 {
		t.Fatalf("ordinary native instructions are missing: %X", cave)
	}
	jump := cave[ordinary+len(startRejectionPromptHookOriginal):]
	if len(jump) < 5 || jump[0] != 0xE9 {
		t.Fatalf("ordinary path has no resume jump: %X", jump)
	}
	displacement := int32(binary.LittleEndian.Uint32(jump[1:5]))
	got := uint32(int64(startRejectionPromptCaveRVA+uint32(ordinary+len(startRejectionPromptHookOriginal)+5)) + int64(displacement))
	if got != startRejectionPromptResumeRVA {
		t.Fatalf("ordinary path resumes at 0x%X, want 0x%X", got, startRejectionPromptResumeRVA)
	}
	if len(cave) > int(startRejectionPromptTextEndRVA-startRejectionPromptCaveRVA) {
		t.Fatalf("cave length %d exceeds reserved tail", len(cave))
	}
}

func TestStartRejectionPromptHookSignature(t *testing.T) {
	if !bytes.Equal(startRejectionPromptHookOriginal, []byte{0x66, 0x8B, 0x03, 0x8B, 0x8F, 0x58, 0xC5, 0x00, 0x00}) {
		t.Fatalf("unexpected room-chat hook signature: %X", startRejectionPromptHookOriginal)
	}
	patched := relativeJump(startRejectionPromptHookRVA, startRejectionPromptCaveRVA, len(startRejectionPromptHookOriginal))
	if len(patched) != 9 || patched[0] != 0xE9 || !bytes.Equal(patched[5:], []byte{0x90, 0x90, 0x90, 0x90}) {
		t.Fatalf("unexpected room-chat hook: %X", patched)
	}
}
