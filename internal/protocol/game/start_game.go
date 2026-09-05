package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	StartGameResultSuccess         uint16 = 0
	StartGameResultPlayersRequired uint16 = 0xF001
	StartGameResultPlayersNotReady uint16 = 0xF002
	StartGameResultTeamInvalid     uint16 = 0xF003
	StartGameResultItemRequired    uint16 = 0xF004
	StartGameResultRejected        uint16 = 0xFFFF
)

// BuildStartGameSuccessPayload is RESPONSE_START_GAME's sole ResultID field.
// Live client traces show that the response retains request command 0x0082.
// REQUEST_PLAY 0x0083 and NOTIFY_GAME_EVENT 0x0084 are separate later routes,
// so neither is valid for this two-byte response object.
func BuildStartGameSuccessPayload() []byte {
	return BuildStartGameResultPayload(StartGameResultSuccess)
}

// BuildStartGameResultPayload serializes RESPONSE_START_GAME's ResultID.
// Values in the 0xF000 private range are used by the local server only; the
// exact human-readable reason is delivered through a companion room-system
// notification so a new reason does not require another client-side text map.
func BuildStartGameResultPayload(resultID uint16) []byte {
	return binary.BigEndian.AppendUint16(nil, resultID)
}

func BuildLocalStartGameSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalStartGameResult(requestPacket, StartGameResultSuccess, nil)
}

func BuildLocalStartGameSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalStartGameResult(requestPacket, StartGameResultSuccess, entropy)
}

func BuildLocalStartGameResult(requestPacket []byte, resultID uint16) ([]byte, error) {
	return buildLocalStartGameResult(requestPacket, resultID, nil)
}

func BuildLocalStartGameResultWithReader(requestPacket []byte, resultID uint16, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalStartGameResult(requestPacket, resultID, entropy)
}

func buildLocalStartGameResult(requestPacket []byte, resultID uint16, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != StartGameCommand {
		return nil, fmt.Errorf("start-game command 0x%04X, want 0x%04X", request.Command, StartGameCommand)
	}
	if len(request.Plaintext) != localInnerHeaderSize+8 {
		return nil, fmt.Errorf("start-game plaintext length %d, want %d", len(request.Plaintext), localInnerHeaderSize+8)
	}
	if binary.BigEndian.Uint32(request.Plaintext[localInnerHeaderSize:localInnerHeaderSize+4]) != request.EnvelopeUIN {
		return nil, fmt.Errorf("start-game payload UIN does not match envelope UIN")
	}
	return buildLocalMessageFromRequest(requestPacket, request, StartGameResponseCommand, BuildStartGameResultPayload(resultID), entropy)
}
