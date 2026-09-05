package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLeaveRoomRequestResponseAndNotification(t *testing.T) {
	payload := make([]byte, leaveRoomRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 0x0A3E20F4)
	plaintext := make([]byte, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:2], LeaveRoomCommand)
	binary.BigEndian.PutUint16(plaintext[8:10], leaveRoomRoute)
	binary.BigEndian.PutUint16(plaintext[10:12], 0xFFFF)
	binary.BigEndian.PutUint16(plaintext[12:14], 1)
	copy(plaintext[localInnerHeaderSize:], payload)
	packet := makeLocalPacketForTest(t, plaintext)
	request, err := DecodeLocalLeaveRoomRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x0A3E20F4 {
		t.Fatalf("leave request = %+v", request)
	}
	response, err := BuildLocalLeaveRoomSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != LeaveRoomCommand || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("leave response command/payload = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
	notification, err := BuildLocalLeaveRoomNotificationWithReader(packet, 7, LeaveRoomNotification{
		PlayerID: 1, NewRoomOwnerID: 2, NewArbitratorID: 2, MapID: 1601,
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err = InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != LeaveRoomNotifyCommand || inspection.Route != leaveRoomRoute || inspection.SectionID != 7 || binary.BigEndian.Uint16(inspection.Payload[2:4]) != 2 {
		t.Fatalf("leave notification = %+v payload=%x", inspection, inspection.Payload)
	}
}
