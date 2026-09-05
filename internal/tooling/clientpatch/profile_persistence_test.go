package clientpatch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestProfilePersistencePayloadFitsReservedCave(t *testing.T) {
	payload := buildProfilePersistencePayload()
	if len(payload) != int(profileCaveSize) {
		t.Fatalf("payload size = 0x%X, want 0x%X", len(payload), profileCaveSize)
	}
	if !bytes.Equal(payload[:len(profileMagic)], profileMagic) {
		t.Fatalf("payload magic = %X, want %X", payload[:len(profileMagic)], profileMagic)
	}
	if allZero(payload[profileEntryStubOffset:]) {
		t.Fatal("profile persistence payload has no executable stubs")
	}
}

func TestProfilePatchSitesTargetReservedStubs(t *testing.T) {
	wants := map[uint32]uint32{
		profileSerializerEntryHook: profileCaveRVA + profileEntryStubOffset,
		profileDestructorCallHook:  profileCaveRVA + profileDestructorStubOffset,
		profileIntegerSetterExit:   profileCaveRVA + profileIntegerSetterStubOffset,
		profileStringSetterExit:    profileCaveRVA + profileStringSetterStubOffset,
		profileBufferSetterExit:    profileCaveRVA + profileBufferSetterStubOffset,
		profileBuffer2SetterExit:   profileCaveRVA + profileBuffer2SetterStubOffset,
		profileDelete1Call:         profileCaveRVA + profileDeleteStubOffset,
		profileStringFreeCall:      profileCaveRVA + profileStringFreeStubOffset,
		profileDelete2Call:         profileCaveRVA + profileDeleteStubOffset,
		profileDelete3Call:         profileCaveRVA + profileDeleteStubOffset,
		profileDelete4Call:         profileCaveRVA + profileDeleteStubOffset,
		profileClearIntegerCall:    profileCaveRVA + profileClearIntegerStubOffset,
		profileClearStringCall:     profileCaveRVA + profileClearStringStubOffset,
		profileClearBufferCall:     profileCaveRVA + profileClearBufferStubOffset,
		profileClearBuffer2Call:    profileCaveRVA + profileClearBuffer2StubOffset,
		profileFinalizeCall:        profileCaveRVA + profileFinalizeStubOffset,
	}
	sites := buildProfilePatchSites()
	if len(sites) != len(wants) {
		t.Fatalf("patch site count = %d, want %d", len(sites), len(wants))
	}
	for _, site := range sites {
		if len(site.patched) < 5 || (site.patched[0] != 0xE8 && site.patched[0] != 0xE9) {
			t.Fatalf("%s patch is not a near call/jump: %X", site.name, site.patched)
		}
		displacement := int32(binary.LittleEndian.Uint32(site.patched[1:5]))
		got := uint32(int64(site.rva+5) + int64(displacement))
		if got != wants[site.rva] {
			t.Fatalf("%s target = 0x%X, want 0x%X", site.name, got, wants[site.rva])
		}
	}
}

func TestProfileFlushModeIsStackLocalAndSettersRequestIt(t *testing.T) {
	payload := buildProfilePersistencePayload()
	entry := payload[profileEntryStubOffset:profileDestructorStubOffset]
	marker := make([]byte, 4)
	binary.LittleEndian.PutUint32(marker, profileFlushMarker)
	if !bytes.Contains(entry, append([]byte{0x81, 0x7D, 0x08}, marker...)) ||
		!bytes.Contains(entry, []byte{0x88, 0x45, 0xD8}) {
		t.Fatalf("serializer entry does not derive [ebp-28h] mode from its argument: %X", entry)
	}
	for _, offset := range []uint32{
		profileIntegerSetterStubOffset,
		profileStringSetterStubOffset,
		profileBufferSetterStubOffset,
		profileBuffer2SetterStubOffset,
	} {
		stub := payload[offset : offset+0x40]
		if !bytes.Contains(stub, append([]byte{0x68}, marker...)) {
			t.Fatalf("setter stub at +0x%X does not push flush marker: %X", offset, stub)
		}
	}
	destructor := payload[profileDestructorStubOffset:profileDeleteStubOffset]
	if !bytes.Contains(destructor, []byte{0x6A, 0x00}) {
		t.Fatalf("manager destructor does not request destructive mode: %X", destructor)
	}
}

func TestProfileConditionalCleanupReturnsOnlyInFlushMode(t *testing.T) {
	payload := buildProfilePersistencePayload()
	for _, offset := range []uint32{
		profileDeleteStubOffset,
		profileStringFreeStubOffset,
		profileClearIntegerStubOffset,
		profileClearStringStubOffset,
		profileClearBufferStubOffset,
		profileClearBuffer2StubOffset,
	} {
		stub := payload[offset : offset+0x20]
		if !bytes.HasPrefix(stub, []byte{0x80, 0x7D, 0xD8, 0x00, 0x74}) || !bytes.Contains(stub, []byte{0xC3, 0xE9}) {
			t.Fatalf("conditional cleanup stub at +0x%X is malformed: %X", offset, stub)
		}
	}
}
