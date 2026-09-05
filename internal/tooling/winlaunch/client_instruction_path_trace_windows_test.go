package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestClientInstructionPathVEHHasBoundedStopControl(t *testing.T) {
	const (
		counter       = uintptr(0x12000800)
		active        = uintptr(0x12000804)
		completed     = uintptr(0x12000808)
		stopRequested = uintptr(0x1200080C)
		records       = uintptr(0x12001000)
		returnAddress = uintptr(0x6F43FBB8)
	)
	handler := buildClientInstructionPathVEH(counter, active, completed, stopRequested, records, 0x11223344, returnAddress)
	if len(handler) >= 0x800 {
		t.Fatalf("instruction path VEH length %d overlaps its control block", len(handler))
	}
	for name, value := range map[string]uintptr{
		"stop request":   stopRequested,
		"record buffer":  records,
		"return address": returnAddress,
	} {
		encoded := make([]byte, 4)
		binary.LittleEndian.PutUint32(encoded, uint32(value))
		if !bytes.Contains(handler, encoded) {
			t.Fatalf("instruction path VEH does not reference %s 0x%08X", name, value)
		}
	}
}

func TestRestoreInstructionPathEFlags(t *testing.T) {
	const unrelated = uint32(0x00000246)
	if got := restoreInstructionPathEFlags(unrelated|0x00010100, 0); got != unrelated {
		t.Fatalf("clearing tracer TF/RF: got 0x%08X want 0x%08X", got, unrelated)
	}
	if got := restoreInstructionPathEFlags(unrelated, 0x00010100); got != unrelated|0x00010100 {
		t.Fatalf("restoring pre-existing TF/RF: got 0x%08X want 0x%08X", got, unrelated|0x00010100)
	}
}
