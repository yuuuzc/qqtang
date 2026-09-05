package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChangeRoleCapturedPayloadVector(t *testing.T) {
	// Captured 2026-08-06 12:22:37: UIN=1000001, client tag/time,
	// and UI-selected role 5 encoded as a uint16.
	payload := []byte{0x00, 0x0f, 0x42, 0x41, 0x00, 0x08, 0x4b, 0x4a, 0x00, 0x05}
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ChangeRoleCommand)
	packet := makeLocalPacketForTest(t, append(plaintext, payload...))

	request, err := DecodeLocalChangeRoleRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x00084B4A || request.RoleID != 5 {
		t.Fatalf("decoded change-role request = %+v", request)
	}

	response, err := BuildLocalChangeRoleSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != ChangeRoleCommand || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("change-role response = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
}

func TestBuildLocalChangeRoleNotification(t *testing.T) {
	payload := make([]byte, changeRolePayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint16(payload[8:10], 23)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ChangeRoleCommand)
	request := makeLocalPacketForTest(t, append(plaintext, payload...))

	packet, err := BuildLocalChangeRoleNotificationWithReader(request, 7, ChangeRoleNotification{
		PlayerUIN: 1_000_001, PlayerID: 1, NewRoleID: 7,
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	wantPayload := []byte{0x00, 0x0F, 0x42, 0x41, 0x00, 0x01, 0x07}
	if inspection.Command != ChangeRoleNotifyCommand || inspection.RouteSequence != 0 || inspection.InnerSequence != 0 || inspection.Route != changeRoleRoomRoute || inspection.SectionID != 7 || !bytes.Equal(inspection.Payload, wantPayload) {
		t.Fatalf("change-role notification = command 0x%04X route %d/%d payload %x", inspection.Command, inspection.RouteSequence, inspection.InnerSequence, inspection.Payload)
	}
	if ChangeRoleNotifyCommand != 0x007B || ChangeRoleNotifySchema != 0x040A || ChangeRoleAckSchema != 0x07F2 {
		t.Fatal("change-role notification constants no longer match QQTMsgData.bin")
	}
}

func TestChangeRoleNotificationRejectsEnvelopeMismatchAndPlaceholder(t *testing.T) {
	payload := make([]byte, changeRolePayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint16(payload[8:10], 7)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ChangeRoleCommand)
	request := makeLocalPacketForTest(t, append(plaintext, payload...))

	if _, err := BuildLocalChangeRoleNotificationWithReader(request, 1, ChangeRoleNotification{
		PlayerUIN: 1_000_002, PlayerID: 1, NewRoleID: 7,
	}, bytes.NewReader(make([]byte, 32))); err == nil {
		t.Fatal("expected envelope UIN mismatch to be rejected")
	}
	if _, err := BuildLocalChangeRoleNotificationWithReader(request, 1, ChangeRoleNotification{
		PlayerUIN: 1_000_001, PlayerID: 1,
	}, bytes.NewReader(make([]byte, 32))); err == nil {
		t.Fatal("expected zero concrete role ID to be rejected")
	}
	if _, err := BuildLocalChangeRoleNotificationWithReader(request, 0, ChangeRoleNotification{
		PlayerUIN: 1_000_001, PlayerID: 1, NewRoleID: 7,
	}, bytes.NewReader(make([]byte, 32))); err == nil {
		t.Fatal("expected zero room ID to be rejected")
	}
}

func TestChangeRoleRejectsOutOfRangeRole(t *testing.T) {
	payload := make([]byte, changeRolePayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint16(payload[8:10], 0x0100)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ChangeRoleCommand)
	if _, err := DecodeLocalChangeRoleRequest(makeLocalPacketForTest(t, append(plaintext, payload...))); err == nil {
		t.Fatal("expected a role ID outside the GAME_BEGIN byte range to be rejected")
	}
}
