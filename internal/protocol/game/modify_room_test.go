package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedModifyRoomNamePlaintext = "00980000000000b70003ffff0002000f42410a5b6ad100000001badac9abb4f3b5d800000000000000000000000000000000000000000000000000000000000000000000"
const capturedModifyRoomPasswordPlaintext = "00980000000001700003ffff0003000f42410a62910400000006badac9abb4f3b5d800000000000000000000000000313233343536000000000000000000000000000000"

func TestDecodeLocalModifyRoomNameRequest(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedModifyRoomNamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	packet := makeLocalPacketForTest(t, plaintext)
	request, err := DecodeLocalModifyRoomRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.Flags != ModifyRoomNameChanged {
		t.Fatalf("request identity/fields = %+v", request)
	}
	name, err := request.RoomNameString()
	if err != nil {
		t.Fatal(err)
	}
	if name != "黑色大地" {
		t.Fatalf("room name = %q", name)
	}
}

func TestDecodeLocalModifyRoomPasswordRequest(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedModifyRoomPasswordPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(
		plaintext[localInnerHeaderSize+8:localInnerHeaderSize+12],
		uint32(ModifyRoomPasswordPresent|ModifyRoomPasswordChanged|ModifyRoomPasswordEnabled),
	)
	plaintext[localInnerHeaderSize+32] = byte(RoomPropertyPassword)
	request, err := DecodeLocalModifyRoomRequest(makeLocalPacketForTest(t, plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if request.Flags != ModifyRoomPasswordPresent|ModifyRoomPasswordChanged|ModifyRoomPasswordEnabled || !request.HasPassword() || request.UsesFreeRule() {
		t.Fatalf("password operation = %+v", request)
	}
	if request.PropertyFlag() != RoomPropertyPassword {
		t.Fatalf("property flag = %d", request.PropertyFlag())
	}
	if got := string(bytes.TrimRight(request.Password[:], "\x00")); got != "123456" {
		t.Fatalf("password = %q", got)
	}
}

func TestDecodeLocalModifyRoomFreeRuleRequest(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedModifyRoomPasswordPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(
		plaintext[localInnerHeaderSize+8:localInnerHeaderSize+12],
		0,
	)
	plaintext[localInnerHeaderSize+32] = byte(RoomPropertyFreeRule)
	clear(plaintext[localInnerHeaderSize+33 : localInnerHeaderSize+49])
	request, err := DecodeLocalModifyRoomRequest(makeLocalPacketForTest(t, plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if request.Flags != 0 || request.HasPassword() || !request.UsesFreeRule() {
		t.Fatalf("free-rule operation = %+v", request)
	}
	if request.PropertyFlag() != RoomPropertyFreeRule {
		t.Fatalf("property flag = %d, want %d", request.PropertyFlag(), RoomPropertyFreeRule)
	}
}

func TestDecodeLocalModifyRoomRejectsUnknownPropertyFlag(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedModifyRoomNamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	plaintext[localInnerHeaderSize+32] = 4
	if _, err := DecodeLocalModifyRoomRequest(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("expected unsupported property flag to be rejected")
	}
}

func TestDecodeLocalModifyRoomRejectsPasswordSlotMismatch(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedModifyRoomPasswordPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	plaintext[localInnerHeaderSize+32] = byte(RoomPropertyStandard)
	if _, err := DecodeLocalModifyRoomRequest(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("expected password slot/property mismatch to be rejected")
	}
}

func TestBuildLocalModifyRoomSuccess(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedModifyRoomNamePlaintext)
	packet := makeLocalPacketForTest(t, plaintext)
	response, err := BuildLocalModifyRoomSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qqtea.Decrypt(response[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(decoded[0:2]) != ModifyRoomCommand {
		t.Fatalf("response command = 0x%04X", binary.BigEndian.Uint16(decoded[0:2]))
	}
	if got := decoded[localInnerHeaderSize:]; !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("response payload = %x", got)
	}
}
