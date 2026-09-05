package game

import "testing"

func TestWorldUseItemRoundTrip(t *testing.T) {
	want := WorldUseItemEvent{
		PlayerID: 2, ClientTime: 99, ItemID: 17, PosX: 120, PosY: 240,
		Flag1: 1, Flag2: 2, Flag3: 3, Flag4: 4,
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseWorldUseItemEvent(GameEvent{Schema: RequestUseItem, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("world use = %+v, want %+v", got, want)
	}
}

func TestPreparedAndWorldItemLayoutsCannotBeConfused(t *testing.T) {
	prepared := PreparedUsePropEvent{PlayerID: 1, ItemID: 20043}
	body, err := prepared.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseWorldUseItemEvent(GameEvent{Schema: RequestUseItem, Body: body}); err == nil {
		t.Fatal("26-byte prepared prop decoded as 30-byte world item")
	}
}
