package winlaunch

import "testing"

func TestResolveRelativeJump32(t *testing.T) {
	target, err := resolveRelativeJump32(0x0042E0C8, []byte{0xE9, 0x33, 0x1F, 0x46, 0x01, 0x90})
	if err != nil {
		t.Fatal(err)
	}
	if target != 0x01890000 {
		t.Fatalf("target = 0x%08X, want 0x01890000", target)
	}
}

func TestResolveRelativeJump32RejectsNonJump(t *testing.T) {
	if _, err := resolveRelativeJump32(0x0042E0C8, []byte{0x90, 0, 0, 0, 0}); err == nil {
		t.Fatal("expected non-jump instruction to be rejected")
	}
}
