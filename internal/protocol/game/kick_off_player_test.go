package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestKickOffPlayerRequestResponseAndRoomNotification(t *testing.T) {
	payload := make([]byte, kickOffPlayerRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 123)
	binary.BigEndian.PutUint16(payload[8:10], 2)
	binary.BigEndian.PutUint32(payload[10:14], 1_000_002)
	plaintext := make([]byte, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:2], KickOffPlayerCommand)
	binary.BigEndian.PutUint16(plaintext[8:10], kickOffPlayerRoute)
	binary.BigEndian.PutUint16(plaintext[10:12], 0xFFFF)
	binary.BigEndian.PutUint16(plaintext[12:14], 1)
	copy(plaintext[localInnerHeaderSize:], payload)
	request := makeLocalPacketForTest(t, plaintext)
	decoded, err := DecodeLocalKickOffPlayerRequest(request)
	if err != nil || decoded.PlayerID != 2 || decoded.PlayerUIN != 1_000_002 {
		t.Fatalf("decoded = %+v, %v", decoded, err)
	}
	response, err := BuildLocalKickOffPlayerResponseWithReader(request, KickOffPlayerFailed, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil || inspection.Command != KickOffPlayerCommand || binary.BigEndian.Uint16(inspection.Payload) != uint16(KickOffPlayerFailed) {
		t.Fatalf("response = %+v, %v", inspection, err)
	}
	notification, err := BuildLocalKickOffRoomNotification(request, 1, KickOffRoomNotification{RoomID: 1, PlayerUIN: 1_000_002})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err = InspectLocalPacket(notification)
	if err != nil || inspection.Command != KickOffRoomNotifyCommand || len(inspection.Payload) != kickOffRoomMinimumSize {
		t.Fatalf("notification = %+v, %v", inspection, err)
	}
}
