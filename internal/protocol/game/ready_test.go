package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadyAndCancelFormalRequestResponseAndNotification(t *testing.T) {
	for _, test := range []struct {
		name    string
		command uint16
		ready   bool
	}{
		{name: "ready", command: ReadyCommand, ready: true},
		{name: "cancel", command: CancelReadyCommand, ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := make([]byte, readyRequestPayloadSize)
			binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
			binary.BigEndian.PutUint32(payload[4:8], 0x10203040)
			plaintext := make([]byte, localInnerHeaderSize)
			binary.BigEndian.PutUint16(plaintext[0:2], test.command)
			requestPacket := makeLocalPacketForTest(t, append(plaintext, payload...))

			request, err := DecodeLocalReadyRequest(requestPacket)
			if err != nil {
				t.Fatal(err)
			}
			if request.UIN != 1_000_001 || request.ClientTime != 0x10203040 || request.Ready != test.ready {
				t.Fatalf("ready request = %+v", request)
			}
			response, err := BuildLocalReadySuccessWithReader(requestPacket, bytes.NewReader(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := InspectLocalPacket(response)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Command != test.command || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
				t.Fatalf("ready response = command 0x%04X payload %x", inspection.Command, inspection.Payload)
			}

			notification, err := BuildLocalReadyStateNotificationWithReader(requestPacket, 3, ReadyStateNotification{
				PlayerUIN: 1_000_001, PlayerID: 2, Ready: test.ready,
			}, bytes.NewReader(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			notifyInspection, err := InspectLocalPacket(notification)
			if err != nil {
				t.Fatal(err)
			}
			wantReady := byte(0)
			if test.ready {
				wantReady = 1
			}
			wantPayload := []byte{0x00, 0x0F, 0x42, 0x41, 0x00, 0x02, wantReady}
			if notifyInspection.Command != ReadyStateNotifyCommand || notifyInspection.Route != readyRoomRoute || notifyInspection.SectionID != 3 || !bytes.Equal(notifyInspection.Payload, wantPayload) {
				t.Fatalf("ready notification = command 0x%04X route %d/%d payload %x", notifyInspection.Command, notifyInspection.Route, notifyInspection.SectionID, notifyInspection.Payload)
			}
		})
	}
	if ReadyRequestSchema != 0x0402 || ReadyResponseSchema != 0x07EA || CancelReadyRequestSchema != 0x0403 || CancelReadyResponseSchema != 0x07EB || ReadyStateNotifySchema != 0x040B || ReadyStateAckSchema != 0x07F3 {
		t.Fatal("ready schema constants no longer match QQTMsgData.bin")
	}
}

func TestReadyRejectsMismatchedEnvelopeUIN(t *testing.T) {
	payload := make([]byte, readyRequestPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 9)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ReadyCommand)
	if _, err := DecodeLocalReadyRequest(makeLocalPacketForTest(t, append(plaintext, payload...))); err == nil {
		t.Fatal("mismatched ready UIN was accepted")
	}
}
