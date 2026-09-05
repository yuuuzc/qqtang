package directory

import (
	"bytes"
	"encoding/binary"
	"testing"

	"qqtang/internal/protocol/qqtea"
)

func TestBuildLocalResponseRoundTrip(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	entropy := bytes.NewReader(bytes.Repeat([]byte{0x5A}, 32))
	packet, err := BuildLocalResponseWithReader(1000001, 0x200, payload, entropy)
	if err != nil {
		t.Fatal(err)
	}
	if got := int(binary.BigEndian.Uint32(packet[:4])); got != len(packet) {
		t.Fatalf("packet length field %d, want %d", got, len(packet))
	}
	if got := binary.BigEndian.Uint32(packet[4:8]); got != 0x200 {
		t.Fatalf("sequence 0x%X", got)
	}
	if got := binary.BigEndian.Uint32(packet[12:16]); got != 1000001 {
		t.Fatalf("uin %d", got)
	}
	if !bytes.Equal(packet[18:50], LocalST) {
		t.Fatalf("ST mismatch: %X", packet[18:50])
	}
	plaintext, err := qqtea.Decrypt(packet[50:], LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{}, DirectoryResponseInnerHeader...), payload...)
	if !bytes.Equal(plaintext, want) {
		t.Fatalf("plaintext %X, want %X", plaintext, want)
	}
}

func TestBuildLocalEmptyResponseLength(t *testing.T) {
	packet, err := BuildLocalResponseWithReader(1000001, 0, nil, bytes.NewReader(bytes.Repeat([]byte{0xA5}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 74 {
		t.Fatalf("packet length %d, want 74", len(packet))
	}
}
