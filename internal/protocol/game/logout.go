package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	LogoutRequestSchema       = 0x03EE // REQUEST_LOGOUT
	LogoutResponseSchema      = 0x07D6 // RESPONSE_LOGOUT
	logoutRequestFixedSize    = 9
	logoutStatisticItemSize   = 5
	logoutMaxStatisticItemNum = 100
)

// LogoutRequest is REQUEST_LOGOUT schema 0x03EE. StatisticItems are retained
// as opaque five-byte records until their individual counters are named from
// a client consumer; their count and framing are already confirmed.
type LogoutRequest struct {
	UIN            uint32
	ClientTime     uint32
	StatisticItems [][]byte
}

func DecodeLocalLogoutRequest(packet []byte) (LogoutRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return LogoutRequest{}, err
	}
	if request.Command != LogoutCommand {
		return LogoutRequest{}, fmt.Errorf("logout command 0x%04X, want 0x%04X", request.Command, LogoutCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) < logoutRequestFixedSize {
		return LogoutRequest{}, fmt.Errorf("logout payload length %d is shorter than %d", len(payload), logoutRequestFixedSize)
	}
	count := int(payload[8])
	if count > logoutMaxStatisticItemNum {
		return LogoutRequest{}, fmt.Errorf("logout statistic item count %d exceeds %d", count, logoutMaxStatisticItemNum)
	}
	want := logoutRequestFixedSize + count*logoutStatisticItemSize
	if len(payload) != want {
		return LogoutRequest{}, fmt.Errorf("logout payload length %d, want %d for %d statistic items", len(payload), want, count)
	}
	decoded := LogoutRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return LogoutRequest{}, fmt.Errorf("logout UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	decoded.StatisticItems = make([][]byte, count)
	for index := range decoded.StatisticItems {
		offset := logoutRequestFixedSize + index*logoutStatisticItemSize
		decoded.StatisticItems[index] = append([]byte(nil), payload[offset:offset+logoutStatisticItemSize]...)
	}
	return decoded, nil
}

func BuildLocalLogoutSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalLogoutSuccess(requestPacket, nil)
}

func BuildLocalLogoutSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalLogoutSuccess(requestPacket, entropy)
}

func buildLocalLogoutSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalLogoutRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, LogoutCommand, []byte{0, 0}, entropy)
}
