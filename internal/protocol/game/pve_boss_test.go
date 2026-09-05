package game

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCreatePVENPCBossRoundTripAndStartNotification(t *testing.T) {
	data := CreatePVENPCBoss{Bosses: []PVEBossInfo{
		{BossID: 30101, BossCount: 2, NormalItems: []BossItemInfo{{ItemID: 30044, ItemCount: 1}}},
		{BossID: 30110, BossCount: 2, NormalItems: []BossItemInfo{{ItemID: 30044, ItemCount: 1}}},
	}}
	wantBody, err := hex.DecodeString("0000000275950200010000755c00010000759e0200010000755c00010000")
	if err != nil {
		t.Fatal(err)
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("CREATE_PVENPC_BOSS body = %x, want %x", body, wantBody)
	}
	decoded, err := ParseCreatePVENPCBossNetwork(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Bosses) != 2 || decoded.Bosses[1].BossID != 30110 || decoded.Bosses[1].NormalItems[0].ItemID != 30044 {
		t.Fatalf("decoded CREATE_PVENPC_BOSS = %+v", decoded)
	}

	plaintext, err := hex.DecodeString(capturedStartGamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, plaintext)
	packet, err := BuildLocalCreatePVENPCBossNotifyWithReader(request, 1, 7, data, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != GameEventNotifyCommand || inspection.RouteSequence != 0 || inspection.InnerSequence != 0 {
		t.Fatalf("notification routing = %+v", inspection)
	}
	notification, err := ParseNotifyGameEventPayload(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Schema != CreatePVENPCBossEvent || notification.RoomID != 1 || notification.GameDataSequence != 7 || !bytes.Equal(notification.Body, wantBody) {
		t.Fatalf("CREATE_PVENPC_BOSS notification = %+v body=%x", notification, notification.Body)
	}
}
