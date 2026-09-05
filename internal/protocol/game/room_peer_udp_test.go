package game

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestDecodeLegacyUDPControlPacketPresenceCapture(t *testing.T) {
	data := mustDecodeUDPHex(t, "001800000000000001000f42410001afe006c0a83801d445")
	packet, err := DecodeLegacyUDPControlPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.PlayerID != 1 || packet.Header.UIN != 1000001 || packet.Header.Type != LegacyUDPPresenceType || packet.Presence == nil {
		t.Fatalf("unexpected presence packet: %+v", packet)
	}
	if packet.Presence.IPv4 != [4]byte{192, 168, 56, 1} || packet.Presence.Port != 54341 {
		t.Fatalf("unexpected presence endpoint: %+v", packet.Presence)
	}
	encoded, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded, data) {
		t.Fatalf("presence round trip\n got %x\nwant %x", encoded, data)
	}
}

func TestDecodeLegacyUDPControlPacketRoomPeerCapture(t *testing.T) {
	data := mustDecodeUDPHex(t, "001e00000000000001000f424100030f7a0c0002000f4242c0a83801f686")
	packet, err := DecodeLegacyUDPControlPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.PlayerID != 1 || packet.Header.UIN != 1000001 || packet.Header.Type != LegacyUDPRoomPeerType || packet.RoomPeer == nil {
		t.Fatalf("unexpected room-peer packet: %+v", packet)
	}
	if packet.RoomPeer.TargetPlayerID != 2 || packet.RoomPeer.TargetUIN != 1000002 {
		t.Fatalf("unexpected target: %+v", packet.RoomPeer)
	}
	if packet.RoomPeer.Endpoint.IPv4 != [4]byte{192, 168, 56, 1} || packet.RoomPeer.Endpoint.Port != 63110 {
		t.Fatalf("unexpected rendezvous endpoint: %+v", packet.RoomPeer.Endpoint)
	}
	encoded, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded, data) {
		t.Fatalf("room-peer round trip\n got %x\nwant %x", encoded, data)
	}
}

func TestDecodeLegacyUDPControlPacketMulticastCapture(t *testing.T) {
	data := mustDecodeUDPHex(t, "005000000000010002000f4242000221bc060001000f4241ab4714790feafddafe3d76c2ba849fd15d02307b0843ac887c61580ae66348f32fe2b36b0e302dc1babb1e2e7c6cb3e7dc7601262f5c9295")
	packet, err := DecodeLegacyUDPControlPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.PlayerID != 2 || packet.Header.UIN != 1000002 || packet.Header.Type != LegacyUDPMulticastType || packet.Multicast == nil {
		t.Fatalf("unexpected multicast packet: %+v", packet)
	}
	wantTarget := LegacyUDPMulticastTarget{PlayerID: 1, UIN: 1_000_001}
	if len(packet.Multicast.Targets) != 1 || packet.Multicast.Targets[0] != wantTarget {
		t.Fatalf("multicast targets = %+v, want %+v", packet.Multicast.Targets, wantTarget)
	}
	if len(packet.Multicast.Data) != 56 {
		t.Fatalf("unexpected BatchData: %x", packet.Multicast.Data)
	}
	encoded, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("multicast round trip\n got %x\nwant %x", encoded, data)
	}
}

func TestLegacyUDPControlPacketRecomputesChecksumAfterEndpointRewrite(t *testing.T) {
	data := mustDecodeUDPHex(t, "001e0000000cf90001000f4241000336960c0002000f4242c0a83801e152")
	packet, err := DecodeLegacyUDPControlPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	packet.RoomPeer.Endpoint.IPv4 = [4]byte{127, 0, 0, 1}
	encoded, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := mustDecodeUDPHex(t, "001e0000000cf90001000f42410003df0f0c0002000f42427f000001e152")
	if !bytes.Equal(encoded, want) {
		t.Fatalf("rewritten room-peer packet\n got %x\nwant %x", encoded, want)
	}
	if _, err := DecodeLegacyUDPControlPacket(encoded); err != nil {
		t.Fatalf("rewritten packet did not validate: %v", err)
	}
}

func TestLegacyUDPRelayDatagramRoundTrip(t *testing.T) {
	payload := mustDecodeUDPHex(t, "001e00000009370002000f424200033b560c0001000f42417f000001e153")
	source := LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: 57683}
	data, err := EncodeLegacyUDPRelayDatagram(source, payload)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := mustDecodeUDPHex(t, "000000017f000001e153")
	if !bytes.Equal(data[:LegacyUDPRelayHeaderSize], wantPrefix) {
		t.Fatalf("relay prefix = %x, want %x", data[:LegacyUDPRelayHeaderSize], wantPrefix)
	}
	decoded, err := DecodeLegacyUDPRelayDatagram(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Source != source || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("decoded relay = %+v payload %x", decoded.Source, decoded.Payload)
	}
}

func TestDecodeLegacyUDPControlPacketRejectsMalformed(t *testing.T) {
	valid := mustDecodeUDPHex(t, "001e00000000000002000f4242000312780c0001000f4241c0a83801f685")
	tests := map[string][]byte{
		"truncated":        valid[:17],
		"declared length":  append([]byte(nil), valid...),
		"payload length":   append([]byte(nil), valid...),
		"self target":      append([]byte(nil), valid...),
		"unsupported type": append([]byte(nil), valid...),
	}
	tests["declared length"][1]--
	tests["payload length"][17]--
	copy(tests["self target"][18:24], tests["self target"][7:13])
	tests["unsupported type"][14] = 9
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeLegacyUDPControlPacket(data); err == nil {
				t.Fatal("expected malformed datagram to be rejected")
			}
		})
	}
}

func mustDecodeUDPHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
