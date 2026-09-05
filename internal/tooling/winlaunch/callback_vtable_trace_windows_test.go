package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestBuildQQTSectionCallbackTraceThunks(t *testing.T) {
	const page = uintptr(0x10000000)
	const common = uintptr(0x10001000)
	thunks := buildQQTSectionCallbackTraceThunks(page, common, qqtSectionCallbackTraceSlots)
	if len(thunks) != qqtSectionCallbackTraceSlots*qqtSectionCallbackThunkSize {
		t.Fatalf("thunk bytes = %d", len(thunks))
	}
	for _, index := range []int{0, 1, qqtSectionCallbackTraceSlots - 1} {
		offset := index * qqtSectionCallbackThunkSize
		if thunks[offset] != 0x68 || binary.LittleEndian.Uint32(thunks[offset+1:offset+5]) != uint32(index) || thunks[offset+5] != 0xE9 {
			t.Fatalf("bad thunk %d: %x", index, thunks[offset:offset+qqtSectionCallbackThunkSize])
		}
		next := page + uintptr(offset+qqtSectionCallbackThunkSize)
		destination := next + uintptr(int32(binary.LittleEndian.Uint32(thunks[offset+6:offset+10])))
		if destination != common {
			t.Fatalf("thunk %d destination = 0x%X", index, destination)
		}
	}
}

func TestBuildQQTSectionCallbackTraceCommon(t *testing.T) {
	stub := buildQQTSectionCallbackTraceCommon(0x12340000, 0x12341000, 0x12342000, 72)
	if len(stub) < 180 {
		t.Fatalf("common stub too short: %d", len(stub))
	}
	if got := stub[len(stub)-3:]; got[0] != 0x61 || got[1] != 0x9D || got[2] != 0xC3 {
		t.Fatalf("common stub does not restore and dispatch: %x", got)
	}
}

func TestBuildQQTSectionCallbackTraceCommonSelectsPointerArgument(t *testing.T) {
	stub := buildQQTSectionCallbackTraceCommonForArgument(0x12340000, 0x12341000, 0x12342000, 1, 3)
	want := []byte{0x8B, 0x74, 0x24, 0x34}
	found := false
	for index := 0; index+len(want) <= len(stub); index++ {
		if string(stub[index:index+len(want)]) == string(want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("third pointer argument load was not emitted: %x", stub)
	}
}

func TestQQTSectionCallbackTraceAllocationFitsAllRecords(t *testing.T) {
	required := qqtSectionCallbackRecordsOffset + qqtSectionCallbackTraceCapacity*qqtSectionCallbackTraceRecordSize
	if required > qqtSectionCallbackAllocationSize {
		t.Fatalf("trace allocation 0x%X is smaller than required 0x%X", qqtSectionCallbackAllocationSize, required)
	}
	if slack := qqtSectionCallbackAllocationSize - required; slack >= 0x10000 {
		t.Fatalf("trace allocation leaves excessive slack: 0x%X", slack)
	}
}

func TestActiveQQTSectionCallbackTracePage(t *testing.T) {
	const page = uintptr(0x10000000)
	const clone = page + 0x2000
	methods := make([]byte, qqtSectionCallbackTraceSlots*4)
	for index := 0; index < qqtSectionCallbackTraceSlots; index++ {
		binary.LittleEndian.PutUint32(methods[index*4:index*4+4], uint32(page+uintptr(index*qqtSectionCallbackThunkSize)))
	}
	got, err := activeQQTSectionCallbackTracePage(clone, methods)
	if err != nil {
		t.Fatal(err)
	}
	if got != page {
		t.Fatalf("trace page = 0x%X, want 0x%X", got, page)
	}
	methods[(qqtSectionCallbackTraceSlots-1)*4]++
	if _, err := activeQQTSectionCallbackTracePage(clone, methods); err == nil {
		t.Fatal("modified clone vtable was accepted")
	}
}

func TestBuildQQTSectionObjectTraceThunksUsesRequestedSlotCount(t *testing.T) {
	const slots = 27
	thunks := buildQQTSectionCallbackTraceThunks(0x10000000, 0x10001000, slots)
	if got, want := len(thunks), slots*qqtSectionCallbackThunkSize; got != want {
		t.Fatalf("thunk bytes = %d, want %d", got, want)
	}
}
