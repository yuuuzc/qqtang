package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestGameMoneyNotificationUsesIndependentCommandAndAbsoluteBalance(t *testing.T) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[:2], GetGameMoneyCommand)
	request := makeLocalPacketForTest(t, append(plaintext, payload...))

	want := GameMoneyNotification{ResultID: 0, MoneyType: 3, Money: MaxGameMoney, UIN: 1_000_001}
	packet, err := BuildLocalGameMoneyNotificationFromRequestWithReader(request, want, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseGameMoneyNotification(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != GameMoneyNotifyCommand || inspection.Command == GetGameMoneyCommand || inspection.Route != localPlayerProfileRoute || got != want {
		t.Fatalf("game-money notification = command 0x%04X route %d data %+v", inspection.Command, inspection.Route, got)
	}
}

func TestGameMoneyNotificationRejectsOverflow(t *testing.T) {
	if _, err := (GameMoneyNotification{UIN: 1, Money: MaxGameMoney + 1}).MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted a game-money notification above the client cap")
	}
}
