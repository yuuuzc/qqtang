package game

import (
	"encoding/binary"
	"fmt"
	"math"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const (
	roomFastEnvelopeSize  = 18
	roomFastSTHeaderSize  = 3
	roomFastMinSTSize     = 5
	roomFastMaxRecipients = 8
	roomFastInnerHeader   = 14
	roomFastEventHeader   = 12
	maxRoomFastEventCount = 64
)

type RoomFastPayloadKind byte

const (
	RoomFastPayloadWaitingRoom RoomFastPayloadKind = iota + 1
	RoomFastPayloadGameplay
)

const roomFastTransferCommand uint16 = 0x0065

// RoomFastEvent is one compact real-time object event. Data remains in its
// native little-endian object layout so unknown future events can be inspected
// without losing bytes.
type RoomFastEvent struct {
	GameTime uint32
	Flag     uint32
	Time     uint32
	DataID   RoomFastEventID
	Sequence uint32
	Data     []byte
}

// RoomFastPacketInspection is the authenticated, decrypted view of one
// waiting-room real-time batch.
type RoomFastPacketInspection struct {
	OuterSequence uint32
	RouteSequence uint16
	EnvelopeUIN   uint32
	Command       uint16
	InnerUnknown  uint32
	InnerSequence uint16
	Route         uint16
	Marker        uint16
	SectionID     uint16
	// RecipientPlayerIDs is the explicit destination list carried by the
	// NetCenter fast-channel service ticket. Player IDs are 16-bit and are not
	// account UINs.
	RecipientPlayerIDs []uint16
	Plaintext          []byte
	// BatchData is the exact QQT_MSG_DATA list carried by the fast upload,
	// starting at the event count. It is retained for decoding and diagnostics;
	// live tests found that wrapping it in an ordinary 0x009D notification does
	// not make the waiting-room simulation consume it.
	BatchData        []byte
	Events           []RoomFastEvent
	PayloadKind      RoomFastPayloadKind
	GameplayPackages []GameplayDataPackage
}

// RoomPlayerMoveInfo is the confirmed ROOM_PLAYER_MOVE_INFO payload. SeatIndex
// is zero-based on the wire, while room.State uses one-based SeatID values.
type RoomPlayerMoveInfo struct {
	SeatIndex byte
	PosX      int32
	PosY      int32
	Direction int32
	Speed     float32
}

// RoomPlayerPutBomb is the confirmed ROOM_PLAYER_PUT_BOMB payload.
type RoomPlayerPutBomb struct {
	SeatIndex byte
	PosX      int32
	PosY      int32
}

// LooksLikeRoomFastPacket deliberately checks only the envelope discriminator.
// Malformed 1/5 frames still enter the strict fast-path decoder instead of
// replacing the last known ordinary packet template.
func LooksLikeRoomFastPacket(packet []byte) bool {
	if len(packet) < roomFastEnvelopeSize+roomFastMinSTSize || packet[16] != 1 {
		return false
	}
	stLength := int(packet[17])
	if stLength < roomFastMinSTSize || stLength > roomFastSTHeaderSize+2*roomFastMaxRecipients || len(packet) < roomFastEnvelopeSize+stLength {
		return false
	}
	st := packet[roomFastEnvelopeSize : roomFastEnvelopeSize+stLength]
	return binary.BigEndian.Uint16(st[0:2]) == roomFastTransferCommand && int(st[2]) >= 1 && stLength == roomFastSTHeaderSize+2*int(st[2])
}

// InspectRoomFastPacket validates and decrypts the compact waiting-room batch
// observed from QQTang 5.2.
func InspectRoomFastPacket(packet []byte) (RoomFastPacketInspection, error) {
	var inspection RoomFastPacketInspection
	if len(packet) < roomFastEnvelopeSize+roomFastMinSTSize+16 {
		return inspection, fmt.Errorf("room fast packet length %d is too short", len(packet))
	}
	declared := int(binary.BigEndian.Uint32(packet[0:4]))
	if declared != len(packet) {
		return inspection, fmt.Errorf("room fast declared length %d != received %d", declared, len(packet))
	}
	if binary.BigEndian.Uint16(packet[10:12]) != 0xFFFF {
		return inspection, fmt.Errorf("room fast outer marker 0x%04X, want 0xFFFF", binary.BigEndian.Uint16(packet[10:12]))
	}
	if packet[16] != 1 {
		return inspection, fmt.Errorf("room fast envelope key marker %d is unsupported", packet[16])
	}
	stLength := int(packet[17])
	if stLength < roomFastMinSTSize || stLength > roomFastSTHeaderSize+2*roomFastMaxRecipients || len(packet) < roomFastEnvelopeSize+stLength+16 {
		return inspection, fmt.Errorf("room fast ST length %d is outside the supported range", stLength)
	}
	st := packet[roomFastEnvelopeSize : roomFastEnvelopeSize+stLength]
	if binary.BigEndian.Uint16(st[0:2]) != roomFastTransferCommand {
		return inspection, fmt.Errorf("room fast ST command 0x%04X, want 0x%04X", binary.BigEndian.Uint16(st[0:2]), roomFastTransferCommand)
	}
	recipientCount := int(st[2])
	if recipientCount < 1 || recipientCount > roomFastMaxRecipients || stLength != roomFastSTHeaderSize+2*recipientCount {
		return inspection, fmt.Errorf("room fast recipient count %d does not match ST length %d", recipientCount, stLength)
	}
	recipients := make([]uint16, 0, recipientCount)
	seenRecipients := make(map[uint16]struct{}, recipientCount)
	for offset := roomFastSTHeaderSize; offset < stLength; offset += 2 {
		playerID := binary.BigEndian.Uint16(st[offset : offset+2])
		if playerID == 0 {
			return inspection, fmt.Errorf("room fast recipient player ID must be non-zero")
		}
		if _, exists := seenRecipients[playerID]; exists {
			return inspection, fmt.Errorf("room fast recipient player ID %d is duplicated", playerID)
		}
		seenRecipients[playerID] = struct{}{}
		recipients = append(recipients, playerID)
	}
	encryptedAt := roomFastEnvelopeSize + stLength
	plaintext, err := qqtea.Decrypt(packet[encryptedAt:], directory.LocalKey)
	if err != nil {
		return inspection, fmt.Errorf("decrypt room fast packet: %w", err)
	}
	if len(plaintext) < roomFastInnerHeader+1 {
		return inspection, fmt.Errorf("room fast plaintext length %d is too short", len(plaintext))
	}
	inspection = RoomFastPacketInspection{
		OuterSequence:      binary.BigEndian.Uint32(packet[4:8]),
		RouteSequence:      binary.BigEndian.Uint16(packet[8:10]),
		EnvelopeUIN:        binary.BigEndian.Uint32(packet[12:16]),
		Command:            binary.BigEndian.Uint16(plaintext[0:2]),
		InnerUnknown:       binary.BigEndian.Uint32(plaintext[2:6]),
		InnerSequence:      binary.BigEndian.Uint16(plaintext[6:8]),
		Route:              binary.BigEndian.Uint16(plaintext[8:10]),
		Marker:             binary.BigEndian.Uint16(plaintext[10:12]),
		SectionID:          binary.BigEndian.Uint16(plaintext[12:14]),
		RecipientPlayerIDs: recipients,
		Plaintext:          append([]byte(nil), plaintext...),
	}
	if inspection.Command != 0 || inspection.InnerUnknown != 0 || inspection.Route != 1 || inspection.Marker != 0xFFFF || inspection.SectionID != 1 {
		return RoomFastPacketInspection{}, fmt.Errorf(
			"room fast inner header command=0x%04X unknown=%d route=%d marker=0x%04X section=%d is unsupported",
			inspection.Command, inspection.InnerUnknown, inspection.Route, inspection.Marker, inspection.SectionID,
		)
	}
	if inspection.InnerSequence != inspection.RouteSequence {
		return RoomFastPacketInspection{}, fmt.Errorf("room fast inner sequence %d != route sequence %d", inspection.InnerSequence, inspection.RouteSequence)
	}

	payload := plaintext[roomFastInnerHeader:]
	inspection.BatchData = append([]byte(nil), payload...)
	count := int(payload[0])
	if count == 0 || count > maxRoomFastEventCount {
		return RoomFastPacketInspection{}, fmt.Errorf("room fast event count %d is outside 1..%d", count, maxRoomFastEventCount)
	}
	offset := 1
	inspection.Events = make([]RoomFastEvent, 0, count)
	inspection.GameplayPackages = make([]GameplayDataPackage, 0, count)
	var payloadKind RoomFastPayloadKind
	for index := 0; index < count; index++ {
		if len(payload)-offset < 10 {
			return RoomFastPacketInspection{}, fmt.Errorf("room fast event %d batch header is truncated", index)
		}
		gameTime := binary.BigEndian.Uint32(payload[offset : offset+4])
		flag := binary.BigEndian.Uint32(payload[offset+4 : offset+8])
		compactLength := int(binary.BigEndian.Uint16(payload[offset+8 : offset+10]))
		offset += 10
		if compactLength < roomFastEventHeader || compactLength > len(payload)-offset {
			return RoomFastPacketInspection{}, fmt.Errorf("room fast event %d compact length %d exceeds remaining %d", index, compactLength, len(payload)-offset)
		}
		compact := payload[offset : offset+compactLength]
		if gameplay, gameplayErr := ParseGameplayDataPackage(compact); gameplayErr == nil {
			if payloadKind == RoomFastPayloadWaitingRoom {
				return RoomFastPacketInspection{}, fmt.Errorf("room fast packet mixes waiting-room and gameplay payloads")
			}
			payloadKind = RoomFastPayloadGameplay
			inspection.GameplayPackages = append(inspection.GameplayPackages, gameplay)
		} else {
			if compactLength < roomFastEventHeader {
				return RoomFastPacketInspection{}, fmt.Errorf("room fast event %d compact length %d is shorter than %d", index, compactLength, roomFastEventHeader)
			}
			dataLength := int(binary.BigEndian.Uint16(compact[6:8]))
			if compactLength != roomFastEventHeader+dataLength {
				return RoomFastPacketInspection{}, fmt.Errorf("room fast event %d is neither QQT_DATA_PACKAGE (%v) nor ROOM_MSG_DATA (length %d != %d + %d)", index, gameplayErr, compactLength, roomFastEventHeader, dataLength)
			}
			if payloadKind == RoomFastPayloadGameplay {
				return RoomFastPacketInspection{}, fmt.Errorf("room fast packet mixes gameplay and waiting-room payloads")
			}
			payloadKind = RoomFastPayloadWaitingRoom
			inspection.Events = append(inspection.Events, RoomFastEvent{
				GameTime: gameTime,
				Flag:     flag,
				Time:     binary.BigEndian.Uint32(compact[0:4]),
				DataID:   RoomFastEventID(binary.BigEndian.Uint16(compact[4:6])),
				Sequence: binary.BigEndian.Uint32(compact[8:12]),
				Data:     append([]byte(nil), compact[12:]...),
			})
		}
		offset += compactLength
	}
	if offset != len(payload) {
		return RoomFastPacketInspection{}, fmt.Errorf("room fast packet has %d trailing plaintext bytes", len(payload)-offset)
	}
	inspection.PayloadKind = payloadKind
	return inspection, nil
}

// RewriteGameplayPeerSchemas converts request-only battle message IDs inside
// a QQTPPP Type-2 BatchData payload to their byte-identical peer notification
// forms. The Type-2 datagram is the unique native scene channel; doing this at
// its relay boundary makes every observer consume the transmitted BombID and
// avoids a later reliable duplicate. Waiting-room or unknown compact payloads
// are preserved byte-for-byte.
func RewriteGameplayPeerSchemas(batch []byte) ([]byte, bool, error) {
	if len(batch) < 1 {
		return nil, false, fmt.Errorf("room peer BatchData is empty")
	}
	count := int(batch[0])
	if count == 0 || count > maxRoomFastEventCount {
		return nil, false, fmt.Errorf("room peer BatchData count %d is outside 1..%d", count, maxRoomFastEventCount)
	}
	result := append([]byte(nil), batch...)
	offset := 1
	changed := false
	for index := 0; index < count; index++ {
		if len(result)-offset < 10 {
			return nil, false, fmt.Errorf("room peer BatchData entry %d header is truncated", index)
		}
		compactLength := int(binary.BigEndian.Uint16(result[offset+8 : offset+10]))
		offset += 10
		if compactLength <= 0 || compactLength > len(result)-offset {
			return nil, false, fmt.Errorf("room peer BatchData entry %d length %d exceeds remaining %d", index, compactLength, len(result)-offset)
		}
		compact := result[offset : offset+compactLength]
		gameplay, err := ParseGameplayDataPackage(compact)
		if err == nil {
			entryChanged := false
			for messageIndex := range gameplay.Messages {
				if gameplay.Messages[messageIndex].DataID == uint32(PlayerUseBomb) {
					gameplay.Messages[messageIndex].DataID = uint32(NotifyPlayerUseBomb)
					entryChanged = true
				}
			}
			if entryChanged {
				encoded, marshalErr := gameplay.MarshalNetworkBinary()
				if marshalErr != nil {
					return nil, false, fmt.Errorf("rewrite room peer gameplay entry %d: %w", index, marshalErr)
				}
				if len(encoded) != compactLength {
					return nil, false, fmt.Errorf("rewritten room peer gameplay entry %d length %d != %d", index, len(encoded), compactLength)
				}
				copy(compact, encoded)
				changed = true
			}
		}
		offset += compactLength
	}
	if offset != len(result) {
		return nil, false, fmt.Errorf("room peer BatchData has %d trailing bytes", len(result)-offset)
	}
	return result, changed, nil
}

// BuildRoomFastRelayPacketForRecipient preserves a captured uploader frame
// while rebinding its outer UIN. It is retained for forensic comparison while
// the native P2P receive route is recovered; live tests show that the client
// does not consume this uploader frame as an ordinary TCP downlink.
func BuildRoomFastRelayPacketForRecipient(packet []byte, recipientUIN uint32) ([]byte, error) {
	if recipientUIN == 0 {
		return nil, fmt.Errorf("room fast recipient UIN must be non-zero")
	}
	if _, err := InspectRoomFastPacket(packet); err != nil {
		return nil, err
	}
	relay := append([]byte(nil), packet...)
	binary.BigEndian.PutUint32(relay[12:16], recipientUIN)
	return relay, nil
}

func DecodeRoomPlayerMoveInfo(data []byte) (RoomPlayerMoveInfo, error) {
	if len(data) != 17 {
		return RoomPlayerMoveInfo{}, fmt.Errorf("ROOM_PLAYER_MOVE_INFO data length %d, want 17", len(data))
	}
	return RoomPlayerMoveInfo{
		SeatIndex: data[0],
		PosX:      int32(binary.LittleEndian.Uint32(data[1:5])),
		PosY:      int32(binary.LittleEndian.Uint32(data[5:9])),
		Direction: int32(binary.LittleEndian.Uint32(data[9:13])),
		Speed:     math.Float32frombits(binary.LittleEndian.Uint32(data[13:17])),
	}, nil
}

func DecodeRoomPlayerPutBomb(data []byte) (RoomPlayerPutBomb, error) {
	if len(data) != 9 {
		return RoomPlayerPutBomb{}, fmt.Errorf("ROOM_PLAYER_PUT_BOMB data length %d, want 9", len(data))
	}
	return RoomPlayerPutBomb{
		SeatIndex: data[0],
		PosX:      int32(binary.LittleEndian.Uint32(data[1:5])),
		PosY:      int32(binary.LittleEndian.Uint32(data[5:9])),
	}, nil
}
