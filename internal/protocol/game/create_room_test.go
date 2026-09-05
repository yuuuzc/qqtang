package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedCreateRoomPlaintext = "006b00000000075e0002ffff0001000f42410f04047bccc7b9fbb1c8cee4b3a10000000000000000000002000000000000000000000000000000000200000001"

func TestBuildCreateRoomSuccessPayloadNativeVector(t *testing.T) {
	payload, err := BuildCreateRoomSuccessPayload(1)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("00000001")
	if !bytes.Equal(payload, want) {
		t.Fatalf("create-room payload = %x, want %x", payload, want)
	}
}

func TestBuildLocalCreateRoomSuccess(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedCreateRoomPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	requestPacket := makeLocalPacketForTest(t, plaintext)
	request, err := DecodeLocalCreateRoomRequest(requestPacket)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.Time != 0x0F04047B || request.GameType != 2 || request.ContinueID != 1 {
		t.Fatalf("decoded request = %+v", request)
	}
	if request.Flag != RoomPropertyFreeRule || request.HasPassword() || !request.UsesFreeRule() {
		t.Fatalf("decoded room properties = 0x%02X", request.Flag)
	}
	response, err := BuildLocalCreateRoomSuccessWithReader(requestPacket, 1, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	responsePlaintext, err := qqtea.Decrypt(response[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(responsePlaintext[localInnerHeaderSize+2:]); got != 1 {
		t.Fatalf("response room ID = %d", got)
	}
	if !bytes.Equal(responsePlaintext[:localInnerHeaderSize], plaintext[:localInnerHeaderSize]) {
		t.Fatal("response route header changed")
	}
}

func TestDecodeLocalCreateRoomRejectsInconsistentProperties(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedCreateRoomPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	// The property byte is at inner-header + 28. Enabling a password without
	// supplying the password slot must not create a room whose list card and
	// authoritative enter gate disagree.
	plaintext[localInnerHeaderSize+28] = byte(RoomPropertyPassword | RoomPropertyFreeRule)
	if _, err = DecodeLocalCreateRoomRequest(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("inconsistent create-room password properties were accepted")
	}
}

func TestBuildCreateRoomSuccessPayloadRejectsZeroRoom(t *testing.T) {
	if _, err := BuildCreateRoomSuccessPayload(0); err == nil {
		t.Fatal("expected room ID zero to be rejected")
	}
}
