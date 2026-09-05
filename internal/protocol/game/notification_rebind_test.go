package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestRebindLocalServerNotificationUsesRecipientEnvelope(t *testing.T) {
	requestPlaintext, err := hex.DecodeString(capturedStartGamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	sourceRequest := makeLocalPacketForTest(t, requestPlaintext)
	source, err := BuildLocalAdventureGameBeginWithReader(sourceRequest, testGameBeginData(), bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}

	recipient := append([]byte(nil), sourceRequest...)
	binary.BigEndian.PutUint32(recipient[4:8], 0x11223344)
	binary.BigEndian.PutUint16(recipient[8:10], 0x5566)
	binary.BigEndian.PutUint32(recipient[12:16], 1_000_002)
	recipientDecoded, err := decodeLocalPacket(recipient)
	if err != nil {
		t.Fatal(err)
	}
	personalRoutePacket, err := buildLocalMessage(
		recipient, recipientDecoded, recipientDecoded.Command,
		recipientDecoded.Plaintext[localInnerHeaderSize:], bytes.NewReader(make([]byte, 64)), false,
		&localMessageRoute{Route: 4, SectionID: 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := RebindLocalServerNotification(personalRoutePacket, source)
	if err != nil {
		t.Fatal(err)
	}
	decodedSource, err := InspectLocalPacket(source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := InspectLocalPacket(rebound)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EnvelopeUIN != 1_000_002 || decoded.OuterSequence != 0x11223344 || decoded.RouteSequence != 0x5566 {
		t.Fatalf("recipient envelope = UIN:%d outer:%08X route:%04X", decoded.EnvelopeUIN, decoded.OuterSequence, decoded.RouteSequence)
	}
	if decoded.Command != GameBeginNotifyCommand || !bytes.Equal(decoded.Payload, decodedSource.Payload) {
		t.Fatalf("rebound command/payload = 0x%04X/%x", decoded.Command, decoded.Payload)
	}
	if decoded.Route != decodedSource.Route || decoded.SectionID != decodedSource.SectionID {
		t.Fatalf("rebound begin route/section = %d/%d, want source room route %d/%d", decoded.Route, decoded.SectionID, decodedSource.Route, decodedSource.SectionID)
	}
}

func TestRebindGameEventKeepsUnsolicitedSequences(t *testing.T) {
	request, _ := makeDeathEventPacket(t)
	source, _, err := BuildLocalGameEventNotificationWithReader(request, 1, 7, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	requestDecoded, err := decodeLocalPacket(request)
	if err != nil {
		t.Fatal(err)
	}
	sourceDecoded, err := decodeLocalPacket(source)
	if err != nil {
		t.Fatal(err)
	}
	source, err = buildLocalNotificationFromRequestWithRoute(request, requestDecoded, GameEventNotifyCommand, sourceDecoded.Plaintext[localInnerHeaderSize:], localMessageRoute{Route: 3, SectionID: 1}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	recipient := append([]byte(nil), request...)
	binary.BigEndian.PutUint32(recipient[12:16], 1_000_002)
	recipientPlaintext, err := decodeLocalPacket(recipient)
	if err != nil {
		t.Fatal(err)
	}
	personalRoutePacket, err := buildLocalMessage(recipient, recipientPlaintext, recipientPlaintext.Command, recipientPlaintext.Plaintext[localInnerHeaderSize:], bytes.NewReader(make([]byte, 64)), false, &localMessageRoute{Route: 4, SectionID: 2})
	if err != nil {
		t.Fatal(err)
	}
	personalRouteDecoded, err := decodeLocalPacket(personalRoutePacket)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the native client's generic 0x00A7 request wrapper. These
	// request-side flags must never leak into a server notification.
	binary.BigEndian.PutUint32(personalRouteDecoded.Plaintext[2:6], 0x00020000)
	personalRoutePacket, err = buildLocalMessage(
		personalRoutePacket, personalRouteDecoded, personalRouteDecoded.Command,
		personalRouteDecoded.Plaintext[localInnerHeaderSize:], bytes.NewReader(make([]byte, 64)), false, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := RebindLocalServerNotification(personalRoutePacket, source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := InspectLocalPacket(rebound)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EnvelopeUIN != 1_000_002 || decoded.RouteSequence != 0 || decoded.InnerUnknown != 0 || binary.BigEndian.Uint16(decoded.Plaintext[6:8]) != 0 {
		t.Fatalf("rebound event envelope/header = UIN:%d route:%04X unknown:%08X inner:%04X", decoded.EnvelopeUIN, decoded.RouteSequence, decoded.InnerUnknown, binary.BigEndian.Uint16(decoded.Plaintext[6:8]))
	}
	if decoded.Route != 3 || decoded.SectionID != 1 {
		t.Fatalf("rebound event route/section = %d/%d, want source room route 3/1", decoded.Route, decoded.SectionID)
	}
}
