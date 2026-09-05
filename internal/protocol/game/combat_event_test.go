package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPlayerExplodedUsesDistinctRequestAndNotifySchemas(t *testing.T) {
	body := []byte{0, 2, 0, 0, 0, 9, 0, 10, 0, 20, 1}
	event, err := ParsePlayerExplodedEvent(GameEvent{Schema: PlayerBeExploded, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if event.PlayerID != 2 || event.ClientTime != 9 || event.PosX != 10 || event.PosY != 20 || !event.IsAvatar {
		t.Fatalf("player exploded = %+v", event)
	}
	if _, err = ParsePlayerExplodedEvent(GameEvent{Schema: NotifyPlayerExploded, Body: body}); err == nil {
		t.Fatal("server notification schema was accepted as a client request")
	}
}

func TestParseAdjacentPlayerCombatEvents(t *testing.T) {
	body := make([]byte, 12)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint32(body[2:6], 0x11223344)
	binary.BigEndian.PutUint16(body[6:8], 2)
	binary.BigEndian.PutUint16(body[8:10], 120)
	binary.BigEndian.PutUint16(body[10:12], 240)
	for _, schema := range []uint16{RequestKillPlayer, RequestSavePlayer, NotifyPlayerSaved} {
		got, err := ParsePlayerInteractionEvent(GameEvent{Schema: schema, Body: body})
		if err != nil {
			t.Fatalf("schema 0x%04X: %v", schema, err)
		}
		if got.PlayerID != 1 || got.DestinationPlayerID != 2 || got.ClientTime != 0x11223344 || got.PosX != 120 || got.PosY != 240 {
			t.Fatalf("schema 0x%04X decoded %+v", schema, got)
		}
	}

	killedBody := append(append([]byte(nil), body...), 1)
	killedBody = binary.BigEndian.AppendUint32(killedBody, 30001)
	killedBody = append(killedBody, 7, 8)
	killed, err := ParsePlayerKilledEvent(GameEvent{Schema: NotifyPlayerKilled, Body: killedBody})
	if err != nil {
		t.Fatal(err)
	}
	if len(killed.Items) != 1 || killed.Items[0] != (GameItem{ItemID: 30001, Row: 7, Col: 8}) {
		t.Fatalf("killed event = %+v", killed)
	}
}

func TestPlayerCombatEventMarshalRoundTrip(t *testing.T) {
	interaction := PlayerInteractionEvent{PlayerID: 1, ClientTime: 42, DestinationPlayerID: 2, PosX: 120, PosY: 240}
	body, err := interaction.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParsePlayerInteractionEvent(GameEvent{Schema: NotifyPlayerSaved, Body: body})
	if err != nil || decoded != interaction {
		t.Fatalf("interaction round trip = %+v, %v", decoded, err)
	}
	killed := PlayerKilledEvent{PlayerInteractionEvent: interaction, Items: []GameItem{{ItemID: 30001, Row: 7, Col: 8}}}
	body, err = killed.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	decodedKilled, err := ParsePlayerKilledEvent(GameEvent{Schema: NotifyPlayerKilled, Body: body})
	if err != nil || decodedKilled.PlayerInteractionEvent != interaction || !itemsEqual(decodedKilled.Items, killed.Items) {
		t.Fatalf("killed round trip = %+v, %v", decodedKilled, err)
	}
}

func TestPlayerCombatEventsRejectWrongNeighborLayout(t *testing.T) {
	if _, err := ParsePlayerInteractionEvent(GameEvent{Schema: RequestKillPlayer, Body: make([]byte, 11)}); err == nil {
		t.Fatal("truncated kill request unexpectedly decoded")
	}
	if _, err := ParsePlayerKilledEvent(GameEvent{Schema: NotifyPlayerKilled, Body: make([]byte, 12)}); err == nil {
		t.Fatal("killed notification without item count unexpectedly decoded")
	}
	self := make([]byte, 12)
	binary.BigEndian.PutUint16(self[0:2], 1)
	binary.BigEndian.PutUint16(self[6:8], 1)
	if _, err := ParsePlayerInteractionEvent(GameEvent{Schema: RequestSavePlayer, Body: self}); err == nil {
		t.Fatal("self-save event unexpectedly decoded")
	}
}

func TestParseAdjacentPlayerItemEvents(t *testing.T) {
	body := make([]byte, 14)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint32(body[2:6], 42)
	binary.BigEndian.PutUint32(body[6:10], 30007)
	binary.BigEndian.PutUint16(body[10:12], 80)
	binary.BigEndian.PutUint16(body[12:14], 160)
	for _, schema := range []uint16{RequestGetItem, NotifyPlayerGetItem} {
		got, err := ParsePlayerItemEvent(GameEvent{Schema: schema, Body: body})
		if err != nil {
			t.Fatalf("schema 0x%04X: %v", schema, err)
		}
		if got.PlayerID != 1 || got.ItemID != 30007 || got.PosX != 80 || got.PosY != 160 {
			t.Fatalf("schema 0x%04X decoded %+v", schema, got)
		}
	}
}

func TestNPCDropItemEventRoundTrip(t *testing.T) {
	want := NPCDropItemEvent{
		ObjectID: 30037,
		PosX:     400,
		PosY:     520,
		Items: []GameItem{
			{ItemID: 30001, Row: 10, Col: 11},
			{ItemID: 25001, Row: 12, Col: 13},
		},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseNPCDropItemEvent(GameEvent{Schema: NotifyNPCDropItem, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got.ObjectID != want.ObjectID || got.PosX != want.PosX || got.PosY != want.PosY || !itemsEqual(got.Items, want.Items) {
		t.Fatalf("NPC drop = %+v, want %+v", got, want)
	}
	bodyAgain, err := got.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bodyAgain, body) {
		t.Fatalf("NPC drop round trip = %x, want %x", bodyAgain, body)
	}
}
