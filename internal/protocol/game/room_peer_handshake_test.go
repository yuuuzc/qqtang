package game

import (
	"encoding/hex"
	"testing"
)

func TestLegacyUDPPeerHandshakeCaptureRoundTrip(t *testing.T) {
	data, err := hex.DecodeString("000f21e93c000f42417f0000014650")
	if err != nil {
		t.Fatal(err)
	}
	handshake, err := DecodeLegacyUDPPeerHandshake(data)
	if err != nil {
		t.Fatal(err)
	}
	if handshake.Flags != LegacyUDPPeerHandshakeResponse || handshake.UIN != 1_000_001 || handshake.Endpoint != (LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: 18000}) {
		t.Fatalf("decoded handshake = %+v", handshake)
	}
	encoded, err := handshake.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(encoded); got != "000f21e93c000f42417f0000014650" {
		t.Fatalf("encoded handshake = %s", got)
	}
}

func TestLegacyUDPPeerHandshakeRejectsCorruptChecksum(t *testing.T) {
	data, err := hex.DecodeString("000f21e93c000f42417f0000014650")
	if err != nil {
		t.Fatal(err)
	}
	data[8] ^= 1
	if _, err = DecodeLegacyUDPPeerHandshake(data); err == nil {
		t.Fatal("corrupt peer-handshake checksum was accepted")
	}
}
