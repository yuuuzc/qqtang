package winlaunch

import (
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestBuildLocalLoginCallbackPayload(t *testing.T) {
	payload, err := BuildLocalLoginCallbackPayload("LocalPlayer")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(payload), 119; got != want {
		t.Fatalf("payload length = %d, want %d", got, want)
	}
	if payload[2] != 18 || payload[3] != 0 {
		t.Fatalf("profile age/gender = %d/%d, want 18/0", payload[2], payload[3])
	}
	if got := string(payload[5 : 5+len("LocalPlayer")]); got != "LocalPlayer" {
		t.Fatalf("nickname = %q", got)
	}
	if payload[37] != 0x10 || payload[52] != 0x1f {
		t.Fatalf("unexpected GT key boundaries: %02X..%02X", payload[37], payload[52])
	}
	if payload[53] != 32 || payload[54] != 0x20 || payload[85] != 0x3f {
		t.Fatalf("unexpected service ticket layout")
	}
	if payload[86] != 32 || payload[87] != 0x40 || payload[118] != 0x5f {
		t.Fatalf("unexpected HTTP service ticket layout")
	}
}

func TestBuildLocalLoginCallbackPayloadSupportsGBKNameAndGender(t *testing.T) {
	payload, err := BuildLocalLoginCallbackPayloadWithGender("糖一", 1)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("糖一"))
	if got := payload[5 : 5+len(want)]; string(got) != string(want) || payload[3] != 1 {
		t.Fatalf("GBK nickname/gender = %X/%d, want %X/1", got, payload[3], want)
	}
}

func TestBuildLocalLoginCallbackPayloadRejectsOversizeNicknameAndGender(t *testing.T) {
	if _, err := BuildLocalLoginCallbackPayload("01234567890123456789012345678901"); err == nil {
		t.Fatal("oversize nickname unexpectedly succeeded")
	}
	if _, err := BuildLocalLoginCallbackPayloadWithGender("player", 2); err == nil {
		t.Fatal("invalid gender unexpectedly succeeded")
	}
}
