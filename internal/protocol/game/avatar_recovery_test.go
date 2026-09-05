package game

import "testing"

func TestAvatarRecoveryEventRoundTrip(t *testing.T) {
	want := AvatarRecoveryEvent{PlayerID: 2, Time: 12_345, PosX: 340, PosY: 228}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAvatarRecoveryEvent(GameEvent{Schema: NotifyRecoverAvatar, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("avatar recovery = %+v, want %+v", got, want)
	}
	if _, err = ParseAvatarRecoveryEvent(GameEvent{Schema: NotifyRecoverAvatar, Body: body[:9]}); err == nil {
		t.Fatal("short avatar-recovery body was accepted")
	}
}
