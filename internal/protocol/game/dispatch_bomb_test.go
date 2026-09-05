package game

import "testing"

func TestDispatchBombDataRoundTrip(t *testing.T) {
	want := DispatchBombData{
		PlayerID: 7, Time: 12345,
		Bombs: []DispatchBomb{{SceneID: 11, Prop: 8, Row: 4, Col: 5}, {SceneID: 2, Prop: 5, Row: 8, Col: 9}},
		Items: []DispatchBombItem{{ItemID: 20001, Row: 3, Col: 10}},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseDispatchBombEvent(GameEvent{Schema: NotifyDispatchBomb, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerID != want.PlayerID || got.Time != want.Time || len(got.Bombs) != 2 || got.Bombs[1] != want.Bombs[1] || len(got.Items) != 1 || got.Items[0] != want.Items[0] {
		t.Fatalf("dispatch-bomb round trip = %+v, want %+v", got, want)
	}
}

func TestDispatchBombDataRejectsInvalidCellAndTruncation(t *testing.T) {
	if _, err := (DispatchBombData{PlayerID: 1, Bombs: []DispatchBomb{{SceneID: 1, Row: 13}}}).MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted out-of-range bomb cell")
	}
	body, err := (DispatchBombData{PlayerID: 1, Items: []DispatchBombItem{{ItemID: 2, Row: 1, Col: 2}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseDispatchBombEvent(GameEvent{Schema: NotifyDispatchBomb, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("accepted truncated dispatch-bomb item")
	}
}
