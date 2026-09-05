package clientpatch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPatchedFrameRateContext(t *testing.T) {
	patched := patchedFrameRateContext()
	if bytes.Equal(patched, legacyRenderThrottleContext) {
		t.Fatal("frame-rate patch did not change the legacy throttle")
	}
	compare := legacyRenderThrottleCompareRVA - legacyRenderThrottleContextRVA
	if patched[compare] != 0 {
		t.Fatalf("render threshold = %d, want 0", patched[compare])
	}
	target := legacyRenderThrottleTargetRVA - legacyRenderThrottleContextRVA
	if got, want := patched[target:target+4], []byte{0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("render wait target = % X, want % X", got, want)
	}
	if len(frameRatePatchHex()) != len(patched)*2 {
		t.Fatal("hex report length does not match patch length")
	}
}

func TestFrameRatePayloadKeepsPhantomRenderingSeparateFromSampling(t *testing.T) {
	if got, want := len(frameRatePayload), 0x1000; got != want {
		t.Fatalf("timing payload size = 0x%X, want 0x%X", got, want)
	}
	if !bytes.Equal(frameRatePayload[:len(frameRateMagic)], frameRateMagic) {
		t.Fatalf("timing payload magic = %q, want %q", frameRatePayload[:len(frameRateMagic)], frameRateMagic)
	}
	entry := frameRatePayload[frameRatePhantomEntryOffset:]
	if !bytes.Contains(entry, []byte{0x83, 0xF8, 0x21}) {
		t.Fatal("phantom adapter has no 33 ms sampling comparison")
	}
	// The draw-only path must return to Client+0x4B53B rather than returning
	// from the object update. This is what preserves per-frame clone and
	// lightning/fire overlay rendering between history samples.
	drawTarget := frameRateImageBase + 0x4B53B
	found := false
	for offset := 0; offset+5 <= len(entry); offset++ {
		if entry[offset] != 0xE9 {
			continue
		}
		displacement := int32(binary.LittleEndian.Uint32(entry[offset+1 : offset+5]))
		sourceNext := int64(frameRateImageBase+frameRateSectionRVA+frameRatePhantomEntryOffset) + int64(offset+5)
		if uint32(sourceNext+int64(displacement)) == drawTarget {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("phantom adapter has no draw-only jump to VA 0x%X", drawTarget)
	}
}

func TestFrameRatePayloadSamplesBananaStopDetectionAtLegacyCadence(t *testing.T) {
	if got, want := frameRatePayload[frameRateBananaCtorEntry:frameRateBananaCtorEntry+13], []byte{
		0xC7, 0x06, 0x94, 0x34, 0x7A, 0x00,
		0xC7, 0x46, 0x08, 0x21, 0x00, 0x00, 0x00,
	}; !bytes.Equal(got, want) {
		t.Fatalf("banana constructor adapter = % X, want % X", got, want)
	}

	entry := frameRatePayload[frameRateBananaTickEntry:]
	// Read elapsed milliseconds from the existing update argument, subtract it
	// from the reserved +8 sampling field, and reload 33 ms after every native
	// position comparison. This must remain periodic rather than startup-only.
	for _, want := range [][]byte{
		{0x8B, 0x44, 0x24, 0x04},
		{0x29, 0x41, 0x08},
		{0xC7, 0x41, 0x08, 0x21, 0x00, 0x00, 0x00},
		{0xC2, 0x04, 0x00},
	} {
		if !bytes.Contains(entry, want) {
			t.Fatalf("banana update adapter has no % X sequence", want)
		}
	}
	assertRelativeJumpTarget(t, entry, frameRateBananaTickEntry, frameRateImageBase+0x1DB531)
}

func assertRelativeJumpTarget(t *testing.T, code []byte, entryOffset uint32, target uint32) {
	t.Helper()
	for offset := 0; offset+5 <= len(code); offset++ {
		if code[offset] != 0xE9 {
			continue
		}
		displacement := int32(binary.LittleEndian.Uint32(code[offset+1 : offset+5]))
		sourceNext := int64(frameRateImageBase+frameRateSectionRVA+entryOffset) + int64(offset+5)
		if uint32(sourceNext+int64(displacement)) == target {
			return
		}
	}
	t.Fatalf("adapter at +0x%X has no jump to VA 0x%X", entryOffset, target)
}
