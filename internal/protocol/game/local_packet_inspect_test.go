package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestInspectLocalPacket(t *testing.T) {
	plaintext := make([]byte, localInnerHeaderSize+3)
	binary.BigEndian.PutUint16(plaintext[:2], PlayerListCommand)
	copy(plaintext[localInnerHeaderSize:], []byte{0xAA, 0xBB, 0xCC})
	request := makeLocalPacketForTest(t, plaintext)
	got, err := InspectLocalPacket(request)
	if err != nil {
		t.Fatalf("InspectLocalPacket: %v", err)
	}
	if got.Command != PlayerListCommand {
		t.Fatalf("command = 0x%04X, want 0x%04X", got.Command, PlayerListCommand)
	}
	if !bytes.Equal(got.Payload, []byte{0xAA, 0xBB, 0xCC}) {
		t.Fatalf("payload = %X", got.Payload)
	}
	got.Payload[0] = 0
	if got.Plaintext[localInnerHeaderSize] != 0xAA {
		t.Fatal("Payload aliases Plaintext")
	}
}
