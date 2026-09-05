package game

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const (
	localInnerHeaderSize = 14
	localEnvelopeSize    = 18
	localSTSize          = 32
	localEncryptedOffset = localEnvelopeSize + localSTSize

	// The login/profile subsystem is independent of room and match routing.
	// Global account notifications emitted during a match must target this
	// route explicitly instead of inheriting the triggering Route=3 packet.
	localPlayerProfileRoute     uint16 = 2
	localPlayerProfileSectionID uint16 = 1
)

type localPacket struct {
	OuterSequence uint32
	RouteSequence uint16
	EnvelopeUIN   uint32
	Command       uint16
	Plaintext     []byte
}

// LocalPacketInspection is the read-only, decrypted view used by protocol
// capture tooling. Payload excludes the 14-byte inner routing header.
type LocalPacketInspection struct {
	OuterSequence uint32
	RouteSequence uint16
	EnvelopeUIN   uint32
	Command       uint16
	InnerUnknown  uint32
	InnerSequence uint16
	Route         uint16
	Marker        uint16
	SectionID     uint16
	Plaintext     []byte
	Payload       []byte
}

// InspectLocalPacket validates and decrypts one packet from the controlled
// local session without exposing the mutable internal packet representation.
func InspectLocalPacket(packet []byte) (LocalPacketInspection, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return LocalPacketInspection{}, err
	}
	plaintext := append([]byte(nil), decoded.Plaintext...)
	return LocalPacketInspection{
		OuterSequence: decoded.OuterSequence,
		RouteSequence: decoded.RouteSequence,
		EnvelopeUIN:   decoded.EnvelopeUIN,
		Command:       decoded.Command,
		InnerUnknown:  binary.BigEndian.Uint32(plaintext[2:6]),
		InnerSequence: binary.BigEndian.Uint16(plaintext[6:8]),
		Route:         binary.BigEndian.Uint16(plaintext[8:10]),
		Marker:        binary.BigEndian.Uint16(plaintext[10:12]),
		SectionID:     binary.BigEndian.Uint16(plaintext[12:14]),
		Plaintext:     plaintext,
		Payload:       append([]byte(nil), plaintext[localInnerHeaderSize:]...),
	}, nil
}

// decodeLocalPacket validates the local ST/key envelope shared by the
// observed game-server requests and decrypts its QQTEA payload.
func decodeLocalPacket(packet []byte) (localPacket, error) {
	var decoded localPacket
	if len(packet) < localEncryptedOffset+16 {
		return decoded, fmt.Errorf("local game packet length %d is too short", len(packet))
	}
	declared := int(binary.BigEndian.Uint32(packet[0:4]))
	if declared != len(packet) {
		return decoded, fmt.Errorf("local game declared length %d != received %d", declared, len(packet))
	}
	if packet[16] != 1 || packet[17] != localSTSize {
		return decoded, fmt.Errorf("local game envelope key/ST marker %d/%d is unsupported", packet[16], packet[17])
	}
	if !bytes.Equal(packet[localEnvelopeSize:localEncryptedOffset], directory.LocalST) {
		return decoded, fmt.Errorf("local game ST does not match the local test session")
	}
	plaintext, err := qqtea.Decrypt(packet[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		return decoded, fmt.Errorf("decrypt local game packet: %w", err)
	}
	if len(plaintext) < localInnerHeaderSize {
		return decoded, fmt.Errorf("local game plaintext length %d is too short", len(plaintext))
	}
	decoded = localPacket{
		OuterSequence: binary.BigEndian.Uint32(packet[4:8]),
		RouteSequence: binary.BigEndian.Uint16(packet[8:10]),
		EnvelopeUIN:   binary.BigEndian.Uint32(packet[12:16]),
		Command:       binary.BigEndian.Uint16(plaintext[0:2]),
		Plaintext:     plaintext,
	}
	return decoded, nil
}

// buildLocalResponse preserves both confirmed routing envelopes and replaces
// only the schema-controlled plaintext payload.
func buildLocalResponse(requestPacket []byte, request localPacket, expectedCommand uint16, payload []byte, entropy io.Reader) ([]byte, error) {
	if request.Command != expectedCommand {
		return nil, fmt.Errorf("local game command 0x%04X, want 0x%04X", request.Command, expectedCommand)
	}
	return buildLocalMessageFromRequest(requestPacket, request, expectedCommand, payload, entropy)
}

// buildLocalMessageFromRequest preserves the authenticated local routing
// envelope while allowing a server-initiated command to use the same live
// connection. This is required for notifications such as game begin, whose
// command differs from the request that caused them.
func buildLocalMessageFromRequest(requestPacket []byte, request localPacket, command uint16, payload []byte, entropy io.Reader) ([]byte, error) {
	return buildLocalMessage(requestPacket, request, command, payload, entropy, false, nil)
}

// buildLocalMessageFromRequestWithRoute preserves the recipient's transport
// correlation while forcing delivery through the source subsystem. Legacy
// request-path callbacks such as GAME_BEGIN require both: they are correlated
// messages rather than unsolicited pushes, but a recipient's most recent
// packet may have come from the personal or lobby dispatcher.
func buildLocalMessageFromRequestWithRoute(requestPacket []byte, request localPacket, command uint16, payload []byte, route localMessageRoute, entropy io.Reader) ([]byte, error) {
	return buildLocalMessage(requestPacket, request, command, payload, entropy, false, &route)
}

// buildLocalNotificationFromRequest creates an unsolicited server push. The
// clear route sequence and encrypted inner route sequence are zero so the
// packet is not correlated with the triggering client request. The server
// payload still has to use its command's direction-specific schema wrapper.
func buildLocalNotificationFromRequest(requestPacket []byte, request localPacket, command uint16, payload []byte, entropy io.Reader) ([]byte, error) {
	return buildLocalMessage(requestPacket, request, command, payload, entropy, true, nil)
}

type localMessageRoute struct {
	Route     uint16
	SectionID uint16
}

// buildLocalNotificationFromRequestWithRoute creates a server push for a
// subsystem other than the request that triggered it. Some global-account
// notifications are emitted while the client is in a room or match; cloning
// that request's route would deliver the command to the room dispatcher.
func buildLocalNotificationFromRequestWithRoute(requestPacket []byte, request localPacket, command uint16, payload []byte, route localMessageRoute, entropy io.Reader) ([]byte, error) {
	return buildLocalMessage(requestPacket, request, command, payload, entropy, true, &route)
}

func buildLocalMessage(requestPacket []byte, request localPacket, command uint16, payload []byte, entropy io.Reader, notification bool, routeOverride *localMessageRoute) ([]byte, error) {
	if len(request.Plaintext) < localInnerHeaderSize {
		return nil, fmt.Errorf("local game plaintext length %d is too short", len(request.Plaintext))
	}
	plaintext := append([]byte(nil), request.Plaintext[:localInnerHeaderSize]...)
	binary.BigEndian.PutUint16(plaintext[0:2], command)
	if notification {
		// Bytes 2..6 are request-side dispatch flags, not part of the
		// authenticated connection identity. In particular, the native client
		// sets 0x00020000 on its generic 0x00A7 wrapper. Copying that value into
		// an unsolicited room notification makes the receiver dispatch the
		// push as a request-derived callback. This was visible after an
		// adventure NEXT_MAP: GAME_OVER sent to a peer inherited 0x00020000 and
		// the peer tore down the room UI instead of returning to the retained
		// room. Server notifications use the canonical zero value.
		binary.BigEndian.PutUint32(plaintext[2:6], 0)
		binary.BigEndian.PutUint16(plaintext[6:8], 0)
	}
	if routeOverride != nil {
		binary.BigEndian.PutUint16(plaintext[8:10], routeOverride.Route)
		binary.BigEndian.PutUint16(plaintext[12:14], routeOverride.SectionID)
	}
	plaintext = append(plaintext, payload...)

	var (
		ciphertext []byte
		err        error
	)
	if entropy == nil {
		ciphertext, err = qqtea.Encrypt(plaintext, directory.LocalKey)
	} else {
		ciphertext, err = qqtea.EncryptWithReader(plaintext, directory.LocalKey, entropy)
	}
	if err != nil {
		return nil, fmt.Errorf("encrypt local game response: %w", err)
	}
	response := make([]byte, localEncryptedOffset+len(ciphertext))
	binary.BigEndian.PutUint32(response[0:4], uint32(len(response)))
	copy(response[4:localEncryptedOffset], requestPacket[4:localEncryptedOffset])
	if notification {
		binary.BigEndian.PutUint16(response[8:10], 0)
	}
	copy(response[localEncryptedOffset:], ciphertext)
	return response, nil
}
