package game

import "testing"

func TestKinFlagIDUsesClientFieldOrder(t *testing.T) {
	flag := NewKinFlagID(7, 23)
	if got, want := flag, (KinFlagID{0, 0, 0, 7, 0, 0, 0, 23}); got != want {
		t.Fatalf("KINFLAGID wire bytes = % X, want % X", got, want)
	}
	if flag.Index() != 7 || flag.FlagID() != 23 {
		t.Fatalf("KINFLAGID fields = index %d, flag %d", flag.Index(), flag.FlagID())
	}
}

func TestKinFlagIDForWireUsesNoKinSentinel(t *testing.T) {
	if got, want := KinFlagIDForWire(0, KinFlagID{}), NewKinFlagID(0, NoKinFlagID); got != want {
		t.Fatalf("no-kin wire flag = %X, want %X", got, want)
	}
	original := NewKinFlagID(7, 23)
	if got := KinFlagIDForWire(9, original); got != original {
		t.Fatalf("kin wire flag = %X, want %X", got, original)
	}
}
