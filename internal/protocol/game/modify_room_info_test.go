package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestModifyRoomInfoSelectedMapVector(t *testing.T) {
	payload := make([]byte, modifyRoomInfoPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 0x11965B74)
	binary.BigEndian.PutUint16(payload[8:10], 1612)
	payload[10] = 0x1A
	binary.BigEndian.PutUint32(payload[11:15], 3)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[:2], ModifyRoomInfoCommand)
	plaintext = append(plaintext, payload...)
	packet := makeLocalPacketForTest(t, plaintext)
	request, err := DecodeLocalModifyRoomInfoRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.MapID != 1612 || request.ContinueID != 3 || request.GameType != 0x1A {
		t.Fatalf("request = %+v", request)
	}
	response, err := BuildLocalModifyRoomInfoSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != ModifyRoomInfoCommand || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("response = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
}
