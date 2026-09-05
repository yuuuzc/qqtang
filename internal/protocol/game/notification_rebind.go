package game

import (
	"encoding/binary"
	"fmt"
)

// RebindLocalServerNotification rebuilds an already encoded match
// notification on another authenticated local-session envelope. Multiplayer
// fan-out must not copy the actor's outer UIN, transport sequence, or encrypted
// inner header to another connection.
//
// The notification payload and source room route are kept byte-for-byte.
// GAME_EVENT remains an unsolicited push with zero correlation sequences.
// GAME_BEGIN keeps the recipient's correlation sequences because this legacy
// command is dispatched through the request callback path, but it must not
// inherit an unrelated personal/lobby route from the recipient's latest packet.
func RebindLocalServerNotification(recipientPacket, sourceNotification []byte) ([]byte, error) {
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, fmt.Errorf("decode recipient packet: %w", err)
	}
	source, err := decodeLocalPacket(sourceNotification)
	if err != nil {
		return nil, fmt.Errorf("decode source notification: %w", err)
	}
	payload := source.Plaintext[localInnerHeaderSize:]
	switch source.Command {
	case GameEventNotifyCommand:
		// GAME_EVENT is room-scoped. Preserve the source notification's route
		// and section while replacing only the recipient envelope; the
		// recipient's latest packet can legitimately be a route-4 personal or
		// lobby request and must not redirect a room event to that dispatcher.
		return buildLocalNotificationFromRequestWithRoute(recipientPacket, recipient, source.Command, payload, localMessageRoute{
			Route:     binary.BigEndian.Uint16(source.Plaintext[8:10]),
			SectionID: binary.BigEndian.Uint16(source.Plaintext[12:14]),
		}, nil)
	case GameBeginNotifyCommand:
		return buildLocalMessageFromRequestWithRoute(recipientPacket, recipient, source.Command, payload, localMessageRoute{
			Route:     binary.BigEndian.Uint16(source.Plaintext[8:10]),
			SectionID: binary.BigEndian.Uint16(source.Plaintext[12:14]),
		}, nil)
	default:
		return nil, fmt.Errorf("command 0x%04X is not a rebindable match notification", source.Command)
	}
}
