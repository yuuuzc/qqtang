package clientpatch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRoomPropertiesCaveWritesWireRoomFlag(t *testing.T) {
	cave := buildRoomPropertiesCave()
	if !bytes.Contains(cave, []byte{0x24, 0x01, 0xD0, 0xE0}) {
		t.Fatalf("room-property cave does not isolate and shift the native rule bit: %X", cave)
	}
	if !bytes.Contains(cave, []byte{0x80, 0x7D, 0x9D, 0x00}) {
		t.Fatalf("room-property cave does not test the copied password string: %X", cave)
	}
	wantWrite := []byte{0x88, 0x45, roomPropertiesFlagOffset}
	if !bytes.Contains(cave, wantWrite) {
		t.Fatalf("room-property cave does not write [ebp-64h]: %X", cave)
	}
	if !bytes.Contains(cave, []byte{0x6A, 0x01, 0x59, 0x89, 0x4D, 0x08}) {
		t.Fatalf("room-property cave does not preserve the explicit UI confirmation: %X", cave)
	}
	if len(cave) > int(roomPropertiesTextEndRVA-roomPropertiesCaveRVA) {
		t.Fatalf("room-property cave length %d exceeds reserved text tail", len(cave))
	}
	jumpOffset := len(cave) - 5
	if cave[jumpOffset] != 0xE9 {
		t.Fatalf("room-property cave tail is not a near jump: %X", cave)
	}
	displacement := int32(binary.LittleEndian.Uint32(cave[jumpOffset+1:]))
	got := uint32(int64(roomPropertiesCaveRVA+uint32(len(cave))) + int64(displacement))
	if got != roomPropertiesResumeRVA {
		t.Fatalf("room-property cave resumes at 0x%X, want 0x%X", got, roomPropertiesResumeRVA)
	}
}

func TestRoomPropertiesLegacyCaveCanBeUpgraded(t *testing.T) {
	for _, legacy := range [][]byte{buildLegacyRoomPropertiesCave(), buildRoomPropertiesCaveV2()} {
		candidate := append(append([]byte(nil), legacy...), make([]byte, len(buildRoomPropertiesCave())-len(legacy))...)
		if !matchesLegacyRoomPropertiesCave(candidate) {
			t.Fatalf("legacy room-property cave was not recognized: %X", candidate)
		}
	}
	if matchesLegacyRoomPropertiesCave(buildRoomPropertiesCave()) {
		t.Fatal("current room-property cave was mistaken for the legacy patch")
	}
	legacy := buildRoomPropertiesCaveV2()
	candidate := append(append([]byte(nil), legacy...), make([]byte, len(buildRoomPropertiesCave())-len(legacy))...)
	candidate[len(candidate)-1] = 1
	if matchesLegacyRoomPropertiesCave(candidate) {
		t.Fatal("non-zero bytes after the legacy cave were accepted")
	}
}

func TestRoomPropertiesHookPreservesOriginalInputLoad(t *testing.T) {
	if !bytes.Equal(roomPropertiesHookOriginal, []byte{0x8A, 0x43, 0x16, 0x83, 0xC4, 0x28}) {
		t.Fatalf("unexpected sc_modifyRoom hook signature: %X", roomPropertiesHookOriginal)
	}
	patched := relativeJump(roomPropertiesHookRVA, roomPropertiesCaveRVA, len(roomPropertiesHookOriginal))
	if len(patched) != len(roomPropertiesHookOriginal) || patched[0] != 0xE9 || patched[5] != 0x90 {
		t.Fatalf("unexpected room-property hook: %X", patched)
	}
}
