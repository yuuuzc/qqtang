package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestLogoutRequestAndResponse(t *testing.T) {
	payload := make([]byte, logoutRequestFixedSize+logoutStatisticItemSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 0x0A3E20F4)
	payload[8] = 1
	copy(payload[9:14], []byte{3, 0, 0, 0, 7})
	plaintext := make([]byte, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:2], LogoutCommand)
	binary.BigEndian.PutUint16(plaintext[8:10], localPlayerProfileRoute)
	binary.BigEndian.PutUint16(plaintext[10:12], 0xFFFF)
	binary.BigEndian.PutUint16(plaintext[12:14], localPlayerProfileSectionID)
	copy(plaintext[localInnerHeaderSize:], payload)
	packet := makeLocalPacketForTest(t, plaintext)

	request, err := DecodeLocalLogoutRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x0A3E20F4 || len(request.StatisticItems) != 1 || !bytes.Equal(request.StatisticItems[0], payload[9:14]) {
		t.Fatalf("logout request = %+v", request)
	}
	response, err := BuildLocalLogoutSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != LogoutCommand || inspection.Route != localPlayerProfileRoute || inspection.SectionID != localPlayerProfileSectionID || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("logout response = %+v payload=%x", inspection, inspection.Payload)
	}
}

func TestDecodeCapturedLogoutBeforeLobbyReconnect(t *testing.T) {
	packet, err := hex.DecodeString("0000005a000000000223ffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3fb3d5ba480b2ca696e93029f1f9c317f6bdce8a5a380383b70f0d76158a470b0be5c67f20ad6f614c")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeLocalLogoutRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x0E2888CC || len(request.StatisticItems) != 0 {
		t.Fatalf("captured logout = %+v", request)
	}
	response, err := BuildLocalLogoutSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != LogoutCommand || inspection.InnerSequence != 547 || inspection.Route != localPlayerProfileRoute || inspection.SectionID != localPlayerProfileSectionID || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("captured logout response = %+v payload=%x", inspection, inspection.Payload)
	}
}

func TestLogoutRequestRejectsCountLengthMismatch(t *testing.T) {
	payload := make([]byte, logoutRequestFixedSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	payload[8] = 1
	plaintext := make([]byte, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:2], LogoutCommand)
	copy(plaintext[localInnerHeaderSize:], payload)
	packet := makeLocalPacketForTest(t, plaintext)
	if _, err := DecodeLocalLogoutRequest(packet); err == nil {
		t.Fatal("expected logout statistic count/length mismatch")
	}
}
