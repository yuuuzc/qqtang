package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestPreparedUsePropCapturedPotionVector(t *testing.T) {
	body, err := hex.DecodeString("000100004e4b035c00640013fd8e000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParsePreparedUsePropEvent(GameEvent{Schema: RequestPreparedUseProp, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.PlayerID != 1 || decoded.ItemID != LargeStaminaPotionItemID || decoded.PosX != 860 || decoded.PosY != 100 || decoded.Flag1 != 0x0013FD8E {
		t.Fatalf("prepared potion = %+v", decoded)
	}
	roundTrip, err := decoded.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, body) {
		t.Fatalf("prepared potion round trip = %x, want %x", roundTrip, body)
	}
}

func TestCancelUsePropRequestAndNotifyShareBody(t *testing.T) {
	body := make([]byte, 10)
	binary.BigEndian.PutUint16(body[0:2], 2)
	binary.BigEndian.PutUint32(body[2:6], 0x10203040)
	binary.BigEndian.PutUint32(body[6:10], 9876)
	event, err := ParseCancelUsePropEvent(GameEvent{Schema: RequestCancelUseProp, Body: body})
	if err != nil || event.PlayerID != 2 || event.PropID != 0x10203040 || event.Time != 9876 {
		t.Fatalf("cancel prop = %+v, %v", event, err)
	}
	roundTrip, err := event.MarshalNetworkBinary()
	if err != nil || !bytes.Equal(roundTrip, body) {
		t.Fatalf("cancel prop round trip = %x, %v", roundTrip, err)
	}
	if _, err = ParseCancelUsePropEvent(GameEvent{Schema: NotifyCancelUseProp, Body: body}); err != nil {
		t.Fatalf("notify cancel prop body rejected: %v", err)
	}
}

func TestParsePropTriggerEventUsesCompactAffectedArray(t *testing.T) {
	body := make([]byte, 24+2*11)
	binary.BigEndian.PutUint32(body[0:4], 0x10203040)
	binary.BigEndian.PutUint32(body[4:8], 9876)
	binary.BigEndian.PutUint16(body[8:10], 1)
	binary.BigEndian.PutUint32(body[10:14], 120)
	binary.BigEndian.PutUint32(body[14:18], 240)
	body[18], body[19] = 3, 2
	offset := 20
	for index, playerID := range []uint16{1, 2} {
		binary.BigEndian.PutUint16(body[offset:offset+2], playerID)
		binary.BigEndian.PutUint32(body[offset+2:offset+6], uint32(300+index))
		binary.BigEndian.PutUint32(body[offset+6:offset+10], uint32(400+index))
		body[offset+10] = byte(index + 1)
		offset += 11
	}
	binary.BigEndian.PutUint32(body[offset:offset+4], 77)
	trigger, err := ParsePropTriggerEvent(GameEvent{Schema: NotifyPropTrigger, Body: body})
	if err != nil || trigger.PropGUID != 0x10203040 || trigger.UserID != 1 || trigger.TypeID != 77 || len(trigger.AffectedPlayers) != 2 || trigger.AffectedPlayers[1].PlayerID != 2 {
		t.Fatalf("prop trigger = %+v, %v", trigger, err)
	}
	body[19] = 11
	if _, err = ParsePropTriggerEvent(GameEvent{Schema: NotifyPropTrigger, Body: body}); err == nil {
		t.Fatal("prop trigger accepted more than ten affected players")
	}
}
