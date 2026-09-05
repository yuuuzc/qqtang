package game

import (
	"encoding/binary"
	"fmt"
)

const (
	// These bounds come from the 181-entry points table in the shipped
	// res/uiRes/levelCFG.pyc (2007-04-10), not from a modern QQTang version.
	MaxPlayerLevel      = 180
	MaxPlayerExperience = 1_631_150_000

	// These values come from the shipped config/pvedata.dat after the client
	// resource loader expands it to its 2851-byte PVEDATA XML. lev_30 is the
	// highest entry and begins at exactly 428326799 adventure points.
	MaxAdventureLevel      = 30
	MaxAdventureExperience = 428_326_799

	// The shipped client renders and seeds the sugar-currency balance as an
	// eight-digit value. Keeping the bound in the shared GAME_INFO domain stops
	// GM edits and future Boss rewards from overflowing different code paths.
	MaxGameMoney = 99_999_999
)

// GAME_INFO is the packed player-statistics structure named by
// QQTMsgData.bin. Only its network layout is used here; no claim is made that
// an unpacked C++ object has the same offsets.
const (
	GameInfoOffsetWinNum      = 0
	GameInfoOffsetLossNum     = GameInfoOffsetWinNum + 4
	GameInfoOffsetEqualNum    = GameInfoOffsetLossNum + 4
	GameInfoOffsetOrgID       = GameInfoOffsetEqualNum + 4
	GameInfoOffsetPoint       = GameInfoOffsetOrgID + 4
	GameInfoOffsetMoney       = GameInfoOffsetPoint + 4
	GameInfoOffsetDegree      = GameInfoOffsetMoney + 4
	GameInfoOffsetRoleID      = GameInfoOffsetDegree + 2
	GameInfoOffsetPetID       = GameInfoOffsetRoleID + 1
	GameInfoOffsetExtWinNum   = GameInfoOffsetPetID + 4
	GameInfoOffsetExtLossNum  = GameInfoOffsetExtWinNum + 4
	GameInfoOffsetExtEqualNum = GameInfoOffsetExtLossNum + 4
	GameInfoOffsetExtPoint    = GameInfoOffsetExtEqualNum + 4
	GameInfoNetworkBinarySize = GameInfoOffsetExtPoint + 4
)

type GameInfo struct {
	WinNum   uint32 `json:"wins"`
	LossNum  uint32 `json:"losses"`
	EqualNum uint32 `json:"draws"`
	OrgID    uint32 `json:"organization_id"`
	Point    uint32 `json:"points"`
	Money    uint32 `json:"money"`
	Degree   uint16 `json:"degree"`
	// RoleID is carried by the legacy GAME_INFO wire schema, but the selected
	// character is an online session/room choice rather than durable account
	// progression. Persistence deliberately clears it before encoding a save.
	RoleID      byte   `json:"role_id,omitempty"`
	PetID       uint32 `json:"pet_id"`
	ExtWinNum   uint32 `json:"extended_wins"`
	ExtLossNum  uint32 `json:"extended_losses"`
	ExtEqualNum uint32 `json:"extended_draws"`
	ExtPoint    uint32 `json:"extended_points"`
}

func (info GameInfo) ValidateLocalClientBounds() error {
	if info.Degree > MaxPlayerLevel {
		return fmt.Errorf("degree %d exceeds client maximum %d", info.Degree, MaxPlayerLevel)
	}
	if info.Point > MaxPlayerExperience {
		return fmt.Errorf("points %d exceeds client maximum %d", info.Point, MaxPlayerExperience)
	}
	if info.ExtPoint > MaxAdventureExperience {
		return fmt.Errorf("extended points %d exceed client adventure maximum %d", info.ExtPoint, MaxAdventureExperience)
	}
	if info.Money > MaxGameMoney {
		return fmt.Errorf("money %d exceeds client maximum %d", info.Money, MaxGameMoney)
	}
	return nil
}

type GameMoneyChange struct {
	Previous uint32
	Awarded  uint32
	Applied  uint32
	Current  uint32
	Capped   bool
}

// ApplyGameMoneyReward is the only addition path for earned sugar currency.
// Purchases subtract in the persistence transaction; rewards saturate here so
// Boss and other special modes cannot wrap the uint32 or exceed the client UI.
func ApplyGameMoneyReward(info GameInfo, awarded uint32) (GameInfo, GameMoneyChange) {
	previous := info.Money
	if previous > MaxGameMoney {
		previous = MaxGameMoney
	}
	current := previous
	if awarded > MaxGameMoney-previous {
		current = MaxGameMoney
	} else {
		current += awarded
	}
	info.Money = current
	return info, GameMoneyChange{
		Previous: previous, Awarded: awarded, Applied: current - previous,
		Current: current, Capped: awarded > current-previous,
	}
}

func (info GameInfo) HasCompletedGame() bool {
	return info.WinNum != 0 || info.LossNum != 0 || info.EqualNum != 0
}

// WithTutorialCompletionMarker returns a copy that follows QQTSection's
// first-begin gate. Existing statistics are preserved; an otherwise empty
// record receives one neutral draw.
func (info GameInfo) WithTutorialCompletionMarker() GameInfo {
	if !info.HasCompletedGame() {
		info.EqualNum = 1
	}
	return info
}

func (info GameInfo) AppendNetworkBinary(dst []byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, GameInfoNetworkBinarySize)...)
	encoded := dst[start:]
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetWinNum:GameInfoOffsetLossNum], info.WinNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetLossNum:GameInfoOffsetEqualNum], info.LossNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetEqualNum:GameInfoOffsetOrgID], info.EqualNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetOrgID:GameInfoOffsetPoint], info.OrgID)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetPoint:GameInfoOffsetMoney], info.Point)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetMoney:GameInfoOffsetDegree], info.Money)
	binary.BigEndian.PutUint16(encoded[GameInfoOffsetDegree:GameInfoOffsetRoleID], info.Degree)
	encoded[GameInfoOffsetRoleID] = info.RoleID
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetPetID:GameInfoOffsetExtWinNum], info.PetID)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetExtWinNum:GameInfoOffsetExtLossNum], info.ExtWinNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetExtLossNum:GameInfoOffsetExtEqualNum], info.ExtLossNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetExtEqualNum:GameInfoOffsetExtPoint], info.ExtEqualNum)
	binary.BigEndian.PutUint32(encoded[GameInfoOffsetExtPoint:GameInfoNetworkBinarySize], info.ExtPoint)
	return dst
}

func ParseGameInfoNetwork(data []byte) (GameInfo, error) {
	if len(data) < GameInfoNetworkBinarySize {
		return GameInfo{}, fmt.Errorf("GAME_INFO network bytes %d, need %d", len(data), GameInfoNetworkBinarySize)
	}
	return GameInfo{
		WinNum:      binary.BigEndian.Uint32(data[GameInfoOffsetWinNum:GameInfoOffsetLossNum]),
		LossNum:     binary.BigEndian.Uint32(data[GameInfoOffsetLossNum:GameInfoOffsetEqualNum]),
		EqualNum:    binary.BigEndian.Uint32(data[GameInfoOffsetEqualNum:GameInfoOffsetOrgID]),
		OrgID:       binary.BigEndian.Uint32(data[GameInfoOffsetOrgID:GameInfoOffsetPoint]),
		Point:       binary.BigEndian.Uint32(data[GameInfoOffsetPoint:GameInfoOffsetMoney]),
		Money:       binary.BigEndian.Uint32(data[GameInfoOffsetMoney:GameInfoOffsetDegree]),
		Degree:      binary.BigEndian.Uint16(data[GameInfoOffsetDegree:GameInfoOffsetRoleID]),
		RoleID:      data[GameInfoOffsetRoleID],
		PetID:       binary.BigEndian.Uint32(data[GameInfoOffsetPetID:GameInfoOffsetExtWinNum]),
		ExtWinNum:   binary.BigEndian.Uint32(data[GameInfoOffsetExtWinNum:GameInfoOffsetExtLossNum]),
		ExtLossNum:  binary.BigEndian.Uint32(data[GameInfoOffsetExtLossNum:GameInfoOffsetExtEqualNum]),
		ExtEqualNum: binary.BigEndian.Uint32(data[GameInfoOffsetExtEqualNum:GameInfoOffsetExtPoint]),
		ExtPoint:    binary.BigEndian.Uint32(data[GameInfoOffsetExtPoint:GameInfoNetworkBinarySize]),
	}, nil
}

// ParseGameInfoMemory decodes the 32-bit client's packed in-memory GAME_INFO.
// The field order and size match the network structure, while native integers
// use little endian on this x86 client.
func ParseGameInfoMemory(data []byte) (GameInfo, error) {
	if len(data) < GameInfoNetworkBinarySize {
		return GameInfo{}, fmt.Errorf("GAME_INFO memory bytes %d, need %d", len(data), GameInfoNetworkBinarySize)
	}
	return GameInfo{
		WinNum:      binary.LittleEndian.Uint32(data[GameInfoOffsetWinNum:GameInfoOffsetLossNum]),
		LossNum:     binary.LittleEndian.Uint32(data[GameInfoOffsetLossNum:GameInfoOffsetEqualNum]),
		EqualNum:    binary.LittleEndian.Uint32(data[GameInfoOffsetEqualNum:GameInfoOffsetOrgID]),
		OrgID:       binary.LittleEndian.Uint32(data[GameInfoOffsetOrgID:GameInfoOffsetPoint]),
		Point:       binary.LittleEndian.Uint32(data[GameInfoOffsetPoint:GameInfoOffsetMoney]),
		Money:       binary.LittleEndian.Uint32(data[GameInfoOffsetMoney:GameInfoOffsetDegree]),
		Degree:      binary.LittleEndian.Uint16(data[GameInfoOffsetDegree:GameInfoOffsetRoleID]),
		RoleID:      data[GameInfoOffsetRoleID],
		PetID:       binary.LittleEndian.Uint32(data[GameInfoOffsetPetID:GameInfoOffsetExtWinNum]),
		ExtWinNum:   binary.LittleEndian.Uint32(data[GameInfoOffsetExtWinNum:GameInfoOffsetExtLossNum]),
		ExtLossNum:  binary.LittleEndian.Uint32(data[GameInfoOffsetExtLossNum:GameInfoOffsetExtEqualNum]),
		ExtEqualNum: binary.LittleEndian.Uint32(data[GameInfoOffsetExtEqualNum:GameInfoOffsetExtPoint]),
		ExtPoint:    binary.LittleEndian.Uint32(data[GameInfoOffsetExtPoint:GameInfoNetworkBinarySize]),
	}, nil
}
