package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

const capturedStartGamePlaintext = "00820000000000dd0003ffff0001000f42410f45940d"

func TestBuildStartGameSuccessPayload(t *testing.T) {
	if got := BuildStartGameSuccessPayload(); !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("payload = %x, want 0000", got)
	}
}

func TestBuildStartGameResultPayload(t *testing.T) {
	if got := BuildStartGameResultPayload(StartGameResultPlayersNotReady); !bytes.Equal(got, []byte{0xF0, 0x02}) {
		t.Fatalf("payload = %x, want f002", got)
	}
}

func TestBuildLocalStartGameSuccess(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedStartGamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, plaintext)
	response, err := BuildLocalStartGameSuccessWithReader(request, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(response[0:4]) != uint32(len(response)) {
		t.Fatalf("response declared length mismatch: %x", response[:4])
	}
	decoded, err := decodeLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command != StartGameResponseCommand {
		t.Fatalf("response command = 0x%04X", decoded.Command)
	}
	if got := decoded.Plaintext[localInnerHeaderSize:]; !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("response payload = %x, want 0000", got)
	}
}

func TestBuildLocalStartGameResult(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedStartGamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, plaintext)
	response, err := BuildLocalStartGameResultWithReader(request, StartGameResultTeamInvalid, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(decoded.Plaintext[localInnerHeaderSize:]); got != StartGameResultTeamInvalid {
		t.Fatalf("response ResultID = 0x%04X, want 0x%04X", got, StartGameResultTeamInvalid)
	}
}

func TestBuildLocalStartGameSuccessRejectsWrongShape(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedStartGamePlaintext)
	plaintext = plaintext[:len(plaintext)-1]
	if _, err := BuildLocalStartGameSuccess(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("expected malformed start-game payload to be rejected")
	}
}
