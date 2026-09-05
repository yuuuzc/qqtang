package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChangeTeamFormalRequestResponseAndNotification(t *testing.T) {
	payload := make([]byte, changeTeamPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 0x00112233)
	binary.BigEndian.PutUint16(payload[8:10], 6)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ChangeTeamCommand)
	requestPacket := makeLocalPacketForTest(t, append(plaintext, payload...))

	request, err := DecodeLocalChangeTeamRequest(requestPacket)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x00112233 || request.TeamID != 6 {
		t.Fatalf("change-team request = %+v", request)
	}
	response, err := BuildLocalChangeTeamSuccessWithReader(requestPacket, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	responseInspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if responseInspection.Command != ChangeTeamCommand || !bytes.Equal(responseInspection.Payload, []byte{0, 0}) {
		t.Fatalf("change-team response = 0x%04X/%x", responseInspection.Command, responseInspection.Payload)
	}

	notification, err := BuildLocalChangeTeamNotificationWithReader(requestPacket, 9, ChangeTeamNotification{
		PlayerUIN: 1_000_001, PlayerID: 1, NewTeamID: 6,
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	notifyInspection, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	wantNotify := []byte{0x00, 0x0F, 0x42, 0x41, 0x00, 0x01, 0x06}
	if notifyInspection.Command != ChangeTeamNotifyCommand || notifyInspection.Route != changeTeamRoomRoute || notifyInspection.SectionID != 9 || !bytes.Equal(notifyInspection.Payload, wantNotify) {
		t.Fatalf("change-team notification = command 0x%04X route %d/%d payload %x", notifyInspection.Command, notifyInspection.Route, notifyInspection.SectionID, notifyInspection.Payload)
	}
	if ChangeTeamRequestSchema != 0x0400 || ChangeTeamResponseSchema != 0x07E8 || ChangeTeamNotifySchema != 0x0409 || ChangeTeamAckSchema != 0x07F1 {
		t.Fatal("change-team schema constants no longer match QQTMsgData.bin")
	}
}

func TestChangeTeamRejectsValuesOutsideEightTeams(t *testing.T) {
	for _, teamID := range []uint16{0, 9} {
		payload := make([]byte, changeTeamPayloadSize)
		binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
		binary.BigEndian.PutUint16(payload[8:10], teamID)
		plaintext := make([]byte, localInnerHeaderSize)
		binary.BigEndian.PutUint16(plaintext[0:2], ChangeTeamCommand)
		if _, err := DecodeLocalChangeTeamRequest(makeLocalPacketForTest(t, append(plaintext, payload...))); err == nil {
			t.Fatalf("expected TeamID %d to be rejected", teamID)
		}
	}
}
