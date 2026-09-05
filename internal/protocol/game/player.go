package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	PlayerListRequestSchema  = 0x03F1
	PlayerListResponseSchema = 0x07D9
	playerListRequestSize    = 12
	playerListFixedSize      = 3
	PlayerListMaximumCount   = 100
	PlayerNicknameSlotSize   = 20
	playerExtItemMaximum     = 30
	playerKinNameMaximum     = 17
	playerInfoAttachSize     = 8
)

// PlayerListRequest is REQUEST_PLAYER (schema 0x03F1). The current client
// requests at most 30 sorted PlayerIDs from StartPlayerID every ten seconds.
type PlayerListRequest struct {
	UIN           uint32
	Time          uint32
	StartPlayerID uint16
	Number        uint16
}

// PlayerInfoOld mirrors PLAYER_INFO_OLD. Variable arrays are represented by
// slices; their count fields are derived during serialization.
type PlayerInfoOld struct {
	UIN        uint32
	Nickname   string
	PlayerID   uint16
	Gender     byte
	IconID     byte
	Identity   uint32
	GameInfo   GameInfo
	ExtItemIDs []uint32
	KinIndex   uint32
	KinName    string
	KinFlagID  KinFlagID
}

type PlayerInfoAttach struct {
	Honor   uint32
	Attach1 uint32
}

// PlayerListEntry binds the three parallel arrays controlled by PlayerCount
// in RESPONSE_PLAYER_OLD: Players, PlayersAttach, and PATTERN_POINTS.
type PlayerListEntry struct {
	Player   PlayerInfoOld
	Attach   PlayerInfoAttach
	Patterns []PatternPoint
}

func PlayerListEntryFromProfile(uin uint32, profile PlayerProfile) PlayerListEntry {
	return PlayerListEntry{
		Player: PlayerInfoOld{
			UIN: uin, Nickname: profile.Nickname, PlayerID: profile.PlayerID,
			Gender: profile.Gender, IconID: profile.IconID, Identity: profile.Identity,
			GameInfo: profile.GameInfo, KinIndex: profile.KinIndex,
			KinName: profile.KinName, KinFlagID: KinFlagIDForWire(profile.KinIndex, profile.KinFlagID),
		},
		Attach:   PlayerInfoAttach{Honor: profile.Honor},
		Patterns: append([]PatternPoint(nil), profile.PatternPoints...),
	}
}

func DecodeLocalPlayerListRequest(packet []byte) (PlayerListRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return PlayerListRequest{}, err
	}
	if request.Command != PlayerListCommand {
		return PlayerListRequest{}, fmt.Errorf("player-list command 0x%04X, want 0x%04X", request.Command, PlayerListCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != playerListRequestSize {
		return PlayerListRequest{}, fmt.Errorf("player-list request payload length %d, want %d", len(payload), playerListRequestSize)
	}
	decoded := PlayerListRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		StartPlayerID: binary.BigEndian.Uint16(payload[8:10]), Number: binary.BigEndian.Uint16(payload[10:12]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return PlayerListRequest{}, fmt.Errorf("player-list envelope UIN %d != payload UIN %d", request.EnvelopeUIN, decoded.UIN)
	}
	return decoded, nil
}

func (player PlayerInfoOld) MarshalNetworkBinary() ([]byte, error) {
	if player.UIN == 0 || player.PlayerID == 0 {
		return nil, fmt.Errorf("PLAYER_INFO_OLD requires non-zero UIN and PlayerID")
	}
	nicknameBytes, err := encodeLegacyGBKText(player.Nickname, PlayerNicknameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("PLAYER_INFO_OLD nickname: %w", err)
	}
	if len(nicknameBytes) == 0 {
		return nil, fmt.Errorf("PLAYER_INFO_OLD nickname must not be empty")
	}
	if len(player.ExtItemIDs) > playerExtItemMaximum {
		return nil, fmt.Errorf("PLAYER_INFO_OLD external item count %d exceeds %d", len(player.ExtItemIDs), playerExtItemMaximum)
	}
	kinNameBytes, err := encodeLegacyGBKText(player.KinName, playerKinNameMaximum)
	if err != nil {
		return nil, fmt.Errorf("PLAYER_INFO_OLD kin name: %w", err)
	}
	if err := player.GameInfo.ValidateLocalClientBounds(); err != nil {
		return nil, fmt.Errorf("PLAYER_INFO_OLD GAME_INFO: %w", err)
	}
	encoded := make([]byte, 0, 94+4*len(player.ExtItemIDs)+len(kinNameBytes))
	encoded = binary.BigEndian.AppendUint32(encoded, player.UIN)
	nickname := make([]byte, PlayerNicknameSlotSize)
	copy(nickname, nicknameBytes)
	encoded = append(encoded, nickname...)
	encoded = binary.BigEndian.AppendUint16(encoded, player.PlayerID)
	encoded = append(encoded, player.Gender, player.IconID)
	encoded = binary.BigEndian.AppendUint32(encoded, player.Identity)
	encoded = player.GameInfo.AppendNetworkBinary(encoded)
	encoded = append(encoded, byte(len(player.ExtItemIDs)))
	for _, itemID := range player.ExtItemIDs {
		encoded = binary.BigEndian.AppendUint32(encoded, itemID)
	}
	encoded = binary.BigEndian.AppendUint32(encoded, player.KinIndex)
	encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(kinNameBytes)))
	encoded = append(encoded, kinNameBytes...)
	encoded = append(encoded, player.KinFlagID[:]...)
	return encoded, nil
}

func (attach PlayerInfoAttach) AppendNetworkBinary(dst []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, attach.Honor)
	return binary.BigEndian.AppendUint32(dst, attach.Attach1)
}

func appendPatternPoints(dst []byte, patterns []PatternPoint) ([]byte, error) {
	if len(patterns) > patternPointMaxCount {
		return nil, fmt.Errorf("PATTERN_POINTS count %d exceeds %d", len(patterns), patternPointMaxCount)
	}
	dst = append(dst, byte(len(patterns)))
	for _, pattern := range patterns {
		dst = append(dst, byte(pattern.GameMode))
		dst = binary.BigEndian.AppendUint32(dst, pattern.PatternPoint)
		dst = binary.BigEndian.AppendUint16(dst, pattern.PatternLevel)
		dst = binary.BigEndian.AppendUint32(dst, pattern.LevelValue)
	}
	return dst, nil
}

// BuildPlayerListPayload serializes RESPONSE_PLAYER_OLD schema 0x07D9. Its
// three arrays are emitted in schema order, not interleaved per player.
func BuildPlayerListPayload(entries []PlayerListEntry) ([]byte, error) {
	if len(entries) > PlayerListMaximumCount {
		return nil, fmt.Errorf("player-list count %d exceeds %d", len(entries), PlayerListMaximumCount)
	}
	payload := make([]byte, playerListFixedSize)
	payload[2] = byte(len(entries))
	for _, entry := range entries {
		encoded, err := entry.Player.MarshalNetworkBinary()
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	for _, entry := range entries {
		payload = entry.Attach.AppendNetworkBinary(payload)
	}
	for _, entry := range entries {
		var err error
		payload, err = appendPatternPoints(payload, entry.Patterns)
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func BuildEmptyPlayerListPayload() []byte {
	payload, _ := BuildPlayerListPayload(nil)
	return payload
}

func BuildPlayerListResponse(requestPacket []byte, entries []PlayerListEntry) ([]byte, error) {
	return buildPlayerListResponse(requestPacket, entries, nil)
}

func BuildPlayerListResponseWithReader(requestPacket []byte, entries []PlayerListEntry, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildPlayerListResponse(requestPacket, entries, entropy)
}

func buildPlayerListResponse(requestPacket []byte, entries []PlayerListEntry, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalPlayerListRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := BuildPlayerListPayload(entries)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, PlayerListCommand, payload, entropy)
}

func BuildEmptyPlayerListResponse(requestPacket []byte) ([]byte, error) {
	return BuildPlayerListResponse(requestPacket, nil)
}

func BuildEmptyPlayerListResponseWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	return BuildPlayerListResponseWithReader(requestPacket, nil, entropy)
}
