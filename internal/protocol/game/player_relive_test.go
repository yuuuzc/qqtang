package game

import "testing"

func TestPlayerReliveEventRoundTrip(t *testing.T) {
	// 6/30 is emitted by the native adventure item-use path. It is not an
	// ordinary competitive cell and must remain an opaque protocol value.
	want := PlayerReliveEvent{PlayerID: 2, ClientTime: 12_345, Row: 6, Col: 30}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePlayerReliveEvent(GameEvent{Schema: NotifyPlayerRelive, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("player relive = %+v, want %+v", got, want)
	}
}
