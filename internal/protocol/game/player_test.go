package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedPlayerListPlaintext = "006a0000000006040002ffff0001000f42410eea350f0000001e"

func TestBuildEmptyPlayerListPayloadNativeVector(t *testing.T) {
	want, err := hex.DecodeString("000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildEmptyPlayerListPayload(); !bytes.Equal(got, want) {
		t.Fatalf("player-list payload = %x, want %x", got, want)
	}
}

func TestBuildEmptyPlayerListResponse(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedPlayerListPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, plaintext)
	response, err := BuildEmptyPlayerListResponseWithReader(request, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(response[0:4]) != uint32(len(response)) {
		t.Fatalf("response declared length mismatch: %x", response[:4])
	}
	if !bytes.Equal(response[4:localEncryptedOffset], request[4:localEncryptedOffset]) {
		t.Fatal("response did not preserve the observed request envelope")
	}
	responsePlaintext, err := qqtea.Decrypt(response[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(responsePlaintext) != localInnerHeaderSize+playerListFixedSize {
		t.Fatalf("response plaintext length = %d", len(responsePlaintext))
	}
	if !bytes.Equal(responsePlaintext[:localInnerHeaderSize], plaintext[:localInnerHeaderSize]) {
		t.Fatal("response route header changed")
	}
	if !bytes.Equal(responsePlaintext[localInnerHeaderSize:], make([]byte, playerListFixedSize)) {
		t.Fatalf("response payload = %x", responsePlaintext[localInnerHeaderSize:])
	}
}

func TestBuildPlayerListPayloadUsesThreeSchemaOrderedArrays(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.Nickname = "糖一"
	profile.PlayerID = 7
	profile.Honor = 99
	payload, err := BuildPlayerListPayload([]PlayerListEntry{PlayerListEntryFromProfile(1_000_001, profile)})
	if err != nil {
		t.Fatal(err)
	}
	// One minimal PLAYER_INFO_OLD is 94 bytes, followed by the eight-byte
	// attach record and one zero PatternNum byte.
	if got, want := len(payload), 3+94+playerInfoAttachSize+1; got != want {
		t.Fatalf("player-list payload length = %d, want %d", got, want)
	}
	if payload[2] != 1 || binary.BigEndian.Uint32(payload[3:7]) != 1_000_001 {
		t.Fatalf("player-list prefix = %x", payload[:7])
	}
	attachOffset := 3 + 94
	if binary.BigEndian.Uint32(payload[attachOffset:attachOffset+4]) != 99 || payload[len(payload)-1] != 0 {
		t.Fatalf("attach/pattern tail = %x", payload[attachOffset:])
	}
}

func TestPlayerInfoOldEncodesChineseKinNameAsGBK(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.Nickname = "糖一"
	profile.PlayerID = 7
	profile.KinIndex = 9
	profile.KinName = "糖家族"
	profile.KinFlagID = NewKinFlagID(0, 1)

	encoded, err := PlayerListEntryFromProfile(1_000_001, profile).Player.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	// KinIndex, counted KinName, and KinFlagID form the variable tail. 糖家族
	// is six bytes in the client's GBK code page, not nine UTF-8 bytes.
	const kinNameBytes = 6
	tail := len(encoded) - (4 + 2 + kinNameBytes + KinFlagIDSize)
	if got := binary.BigEndian.Uint32(encoded[tail : tail+4]); got != 9 {
		t.Fatalf("kin index = %d", got)
	}
	if got := binary.BigEndian.Uint16(encoded[tail+4 : tail+6]); got != kinNameBytes {
		t.Fatalf("kin name byte count = %d, want %d", got, kinNameBytes)
	}
	wantName := []byte{0xCC, 0xC7, 0xBC, 0xD2, 0xD7, 0xE5}
	if !bytes.Equal(encoded[tail+6:tail+6+kinNameBytes], wantName) {
		t.Fatalf("kin name bytes = %X, want %X", encoded[tail+6:tail+6+kinNameBytes], wantName)
	}
	if !bytes.Equal(encoded[len(encoded)-KinFlagIDSize:], profile.KinFlagID[:]) {
		t.Fatalf("kin flag tail = %X", encoded[len(encoded)-KinFlagIDSize:])
	}
}

func TestBuildEmptyPlayerListResponseRejectsWrongCommand(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedPlayerListPlaintext)
	binary.BigEndian.PutUint16(plaintext[0:2], RoomListCommand)
	if _, err := BuildEmptyPlayerListResponse(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("expected wrong command to be rejected")
	}
}
