package game

import "testing"

func TestBunActionRoundTripPreservesNativeFields(t *testing.T) {
	want := BunActionEvent{PlayerID: 2, ClientTime: 3, PosX: 40, PosY: 50, BunID: 6, BunTeamID: 7}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []uint16{RequestGetBun, NotifyPlayerGetBun, RequestPutBun, NotifyPlayerPutBun} {
		got, err := ParseBunActionEvent(GameEvent{Schema: schema, Body: body})
		if err != nil || got != want {
			t.Fatalf("schema 0x%04X objective action = %+v/%v", schema, got, err)
		}
	}
}
