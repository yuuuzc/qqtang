package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCapturedAdventureDefaultSeatLock(t *testing.T) {
	plaintext := make([]byte, localInnerHeaderSize+setSeatStatusRequestSize)
	binary.BigEndian.PutUint16(plaintext[0:2], SetSeatStatusCommand)
	binary.BigEndian.PutUint16(plaintext[8:10], setSeatStatusRoomRoute)
	binary.BigEndian.PutUint16(plaintext[10:12], 0xFFFF)
	binary.BigEndian.PutUint16(plaintext[12:14], 1)
	binary.BigEndian.PutUint32(plaintext[14:18], 1_000_001)
	binary.BigEndian.PutUint32(plaintext[18:22], 0x0A3BCC26)
	plaintext[22] = 8
	plaintext[23] = byte(RoomSeatStatusLocked)
	packet := makeLocalPacketForTest(t, plaintext)
	request, err := DecodeLocalSetSeatStatusRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.SeatID != 8 || !request.Locked() {
		t.Fatalf("seat request = %+v", request)
	}
	response, err := BuildLocalSetSeatStatusSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != SetSeatStatusCommand || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("seat response command/payload = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
	notification, err := BuildLocalSetSeatStatusNotificationWithReader(packet, 1, 8, RoomSeatStatusLocked, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err = InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != SetSeatStatusNotifyCommand || inspection.Route != setSeatStatusRoomRoute || inspection.SectionID != 1 || !bytes.Equal(inspection.Payload, []byte{8, 2}) {
		t.Fatalf("seat notification = %+v payload=%x", inspection, inspection.Payload)
	}
}
