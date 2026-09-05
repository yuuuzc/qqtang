package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	HelloRequestSchema    = 0x040F
	HelloResponseSchema   = 0x0410
	helloFixedPayloadSize = 10
	helloMaximumInfoSize  = 32
)

type HelloRequest struct {
	UIN  uint32
	Time uint32
	Info []byte
}

func DecodeLocalHelloRequest(packet []byte) (HelloRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return HelloRequest{}, err
	}
	if request.Command != HelloCommand {
		return HelloRequest{}, fmt.Errorf("hello command 0x%04X, want 0x%04X", request.Command, HelloCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) < helloFixedPayloadSize {
		return HelloRequest{}, fmt.Errorf("hello payload length %d is below %d", len(payload), helloFixedPayloadSize)
	}
	infoLength := int(binary.BigEndian.Uint16(payload[8:10]))
	if infoLength > helloMaximumInfoSize || len(payload) != helloFixedPayloadSize+infoLength {
		return HelloRequest{}, fmt.Errorf("hello info length %d does not match payload length %d", infoLength, len(payload))
	}
	decoded := HelloRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		Info: append([]byte(nil), payload[10:]...),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return HelloRequest{}, fmt.Errorf("hello envelope UIN %d != payload UIN %d", request.EnvelopeUIN, decoded.UIN)
	}
	return decoded, nil
}

func BuildLocalHelloSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalHelloSuccess(requestPacket, nil)
}

func buildLocalHelloSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalHelloRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, HelloCommand, []byte{0, 0}, entropy)
}
