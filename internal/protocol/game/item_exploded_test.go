package game

import "testing"

func TestItemsExplodedEventRoundTrip(t *testing.T) {
	want := ItemsExplodedEvent{
		PlayerID: 1,
		Time:     12_345,
		Items: []GameItem{
			{ItemID: 21, Row: 2, Col: 3},
			{ItemID: 43, Row: 7, Col: 8},
		},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseItemsExplodedEvent(GameEvent{Schema: NotifyItemExploded, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerID != want.PlayerID || got.Time != want.Time || len(got.Items) != len(want.Items) || got.Items[0] != want.Items[0] || got.Items[1] != want.Items[1] {
		t.Fatalf("decoded item destruction = %+v, want %+v", got, want)
	}
}

func TestItemsExplodedEventRejectsTruncatedBody(t *testing.T) {
	body, err := (ItemsExplodedEvent{PlayerID: 1, Items: []GameItem{{ItemID: 21, Row: 2, Col: 3}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseItemsExplodedEvent(GameEvent{Schema: NotifyItemExploded, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("truncated NOTIFY_ITEM_BEEXPLODED was accepted")
	}
}
