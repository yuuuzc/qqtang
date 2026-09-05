package game

import "testing"

func TestParseNativeMoveBombRequestNotifyPair(t *testing.T) {
	body := []byte{
		0x00, 0x01, 0x9b, 0x03, 0x00, 0x02, 0x00, 0x08, 0x00, 0x02,
		0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x01, 0x98, 0x82, 0x07,
	}
	for _, schema := range []uint16{RequestMoveBomb, NotifyPlayerMoveBomb} {
		got, err := ParseMoveBombEvent(GameEvent{Schema: schema, Body: body})
		if err != nil {
			t.Fatalf("schema 0x%04X: %v", schema, err)
		}
		if got.PlayerID != 1 || got.ClientTime != 0x9b03 || got.FromRow != 2 || got.FromCol != 8 || got.ToRow != 2 || got.ToCol != 0 || got.Direction != 2 || got.BombPlayerID != 1 || got.BombTime != 0x00019882 || got.BombPower != 7 {
			t.Fatalf("schema 0x%04X decoded %+v", schema, got)
		}
		encoded, err := got.MarshalNetworkBinary()
		if err != nil {
			t.Fatalf("schema 0x%04X marshal: %v", schema, err)
		}
		if string(encoded) != string(body) {
			t.Fatalf("schema 0x%04X round trip = %x, want %x", schema, encoded, body)
		}
	}
}
