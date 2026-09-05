package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestUseItemInRoomTypedRequestResponseAndNotification(t *testing.T) {
	payload := make([]byte, useItemInRoomRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 0x11223344)
	binary.BigEndian.PutUint32(payload[8:12], 24_043)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[:2], UseItemInRoomCommand)
	packet := makeLocalPacketForTest(t, append(plaintext, payload...))

	decoded, err := DecodeLocalUseItemInRoomRequest(packet)
	if err != nil || decoded.UIN != 1_000_001 || decoded.ClientTime != 0x11223344 || decoded.ItemID != 24_043 {
		t.Fatalf("use-item-in-room request = %+v/%v", decoded, err)
	}
	response, err := BuildLocalUseItemInRoomResponseWithReader(packet, 0, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	responseInspection, err := InspectLocalPacket(response)
	if err != nil || responseInspection.Command != UseItemInRoomCommand || !bytes.Equal(responseInspection.Payload, []byte{0, 0}) {
		t.Fatalf("use-item-in-room response = %+v/%v", responseInspection, err)
	}

	notify, err := BuildLocalUseItemInRoomNotificationWithReader(packet, 7, UseItemInRoomNotification{
		UIN: 1_000_001, DialogCode: 3, ItemID: 24_043,
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	notifyInspection, err := InspectLocalPacket(notify)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0x0f, 0x42, 0x41, 0, 3, 0, 0, 0x5d, 0xeb}
	if notifyInspection.Command != UseItemInRoomNotifyCommand || notifyInspection.Route != useItemInRoomRoute || notifyInspection.SectionID != 7 || !bytes.Equal(notifyInspection.Payload, want) {
		t.Fatalf("use-item-in-room notification = command 0x%04X route %d/%d payload %x", notifyInspection.Command, notifyInspection.Route, notifyInspection.SectionID, notifyInspection.Payload)
	}
}

func TestUseItemInRoomIsDistinctFromBattlefieldUse(t *testing.T) {
	if UseItemInRoomRequestSchema == RequestUseItem || useItemInRoomRequestSize == worldUseItemWireSize {
		t.Fatal("room inventory use collapsed into battlefield item use")
	}
}
