package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	CreateNPCBossEvent         = 0x1389
	CompetitiveBossMaxCount    = 8
	CompetitiveBossMaxItems    = 20
	CompetitiveBossMaxOutfits  = 10
	CompetitiveBossMaxSkills   = 10
	CompetitiveBossMaxMetadata = 128
)

// CompetitiveBossInfo mirrors BOSS_INFO from the shipped QQTMsgData.bin.
// The encoder writes its count-delimited arrays compactly; the 446-byte
// schema size is the maximum decoded object size, not a fixed wire stride.
type CompetitiveBossInfo struct {
	Label       uint32
	BossID      uint16
	RoleID      byte
	TeamID      byte
	AIType      uint32
	Row         uint16
	Col         uint16
	HP          uint16
	Rate        uint32
	Bubble      uint32
	Power       uint16
	NormalItems []BossItemInfo
	OutfitItems []BossItemInfo
	Skills      []uint32
	Metadata    []byte
}

// CreateNPCBoss mirrors CREATE_NPC_BOSS (schema 0x1389).
type CreateNPCBoss struct {
	Bosses []CompetitiveBossInfo
	Time   uint32
}

func (data CreateNPCBoss) Validate() error {
	if len(data.Bosses) == 0 || len(data.Bosses) > CompetitiveBossMaxCount {
		return fmt.Errorf("CREATE_NPC_BOSS boss count %d is outside 1..%d", len(data.Bosses), CompetitiveBossMaxCount)
	}
	bossIDs := make(map[uint16]struct{}, len(data.Bosses))
	for index, boss := range data.Bosses {
		// The native rule-8 producer leaves BossLable and HP at zero. BossID is
		// the generated per-entity identity used by the consumer; BossLable is
		// optional presentation metadata rather than an identity key.
		if boss.BossID == 0 || boss.RoleID == 0 || boss.TeamID == 0 {
			return fmt.Errorf("CREATE_NPC_BOSS bosses[%d] needs non-zero boss, role, and team IDs", index)
		}
		if _, duplicate := bossIDs[boss.BossID]; duplicate {
			return fmt.Errorf("CREATE_NPC_BOSS repeats boss ID %d", boss.BossID)
		}
		bossIDs[boss.BossID] = struct{}{}
		// Row/Col are rule-owned rather than schema-owned. Rule 8 transmits a
		// selected free cell and the server validates it at that handler. Rule
		// 1 ignores these fields and selects a free cell locally, so zero is a
		// valid server placeholder for its map-specific boss overlay.
		if err := validateBossItems("normal", boss.Label, boss.NormalItems, CompetitiveBossMaxItems); err != nil {
			return err
		}
		if err := validateBossItems("outfit", boss.Label, boss.OutfitItems, CompetitiveBossMaxOutfits); err != nil {
			return err
		}
		if len(boss.Skills) > CompetitiveBossMaxSkills {
			return fmt.Errorf("CREATE_NPC_BOSS boss label %d skill count %d exceeds %d", boss.Label, len(boss.Skills), CompetitiveBossMaxSkills)
		}
		// Zero is an ignored positional placeholder in the native interpreters,
		// not an invalid schema value. In particular the rule-2 skill 10..12
		// controller aligns cooldown storage by list index, so an encounter that
		// disables skill 10 must still send {0,11,12}. Require at least one real
		// skill when a program is present, but preserve every wire slot verbatim.
		hasSkill := false
		for _, skill := range boss.Skills {
			hasSkill = hasSkill || skill != 0
		}
		if len(boss.Skills) != 0 && !hasSkill {
			return fmt.Errorf("CREATE_NPC_BOSS boss label %d skill program contains only placeholders", boss.Label)
		}
		if len(boss.Metadata) > CompetitiveBossMaxMetadata {
			return fmt.Errorf("CREATE_NPC_BOSS boss label %d metadata length %d exceeds %d", boss.Label, len(boss.Metadata), CompetitiveBossMaxMetadata)
		}
	}
	return nil
}

func validateBossItems(kind string, label uint32, items []BossItemInfo, limit int) error {
	if len(items) > limit {
		return fmt.Errorf("CREATE_NPC_BOSS boss label %d %s item count %d exceeds %d", label, kind, len(items), limit)
	}
	seen := make(map[uint32]struct{}, len(items))
	for index, item := range items {
		if item.ItemID == 0 || item.ItemCount == 0 {
			return fmt.Errorf("CREATE_NPC_BOSS boss label %d %s items[%d] needs non-zero item ID and count", label, kind, index)
		}
		if _, duplicate := seen[item.ItemID]; duplicate {
			return fmt.Errorf("CREATE_NPC_BOSS boss label %d repeats %s item %d", label, kind, item.ItemID)
		}
		seen[item.ItemID] = struct{}{}
	}
	return nil
}

func (data CreateNPCBoss) MarshalNetworkBinary() ([]byte, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, 8+len(data.Bosses)*64)
	encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(data.Bosses)))
	for _, boss := range data.Bosses {
		encoded = binary.BigEndian.AppendUint32(encoded, boss.Label)
		encoded = binary.BigEndian.AppendUint16(encoded, boss.BossID)
		encoded = append(encoded, boss.RoleID, boss.TeamID)
		encoded = binary.BigEndian.AppendUint32(encoded, boss.AIType)
		encoded = binary.BigEndian.AppendUint16(encoded, boss.Row)
		encoded = binary.BigEndian.AppendUint16(encoded, boss.Col)
		encoded = binary.BigEndian.AppendUint16(encoded, boss.HP)
		encoded = binary.BigEndian.AppendUint32(encoded, boss.Rate)
		encoded = binary.BigEndian.AppendUint32(encoded, boss.Bubble)
		encoded = binary.BigEndian.AppendUint16(encoded, boss.Power)
		encoded = appendBossItems(encoded, boss.NormalItems)
		encoded = appendBossItems(encoded, boss.OutfitItems)
		encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(boss.Skills)))
		for _, skill := range boss.Skills {
			encoded = binary.BigEndian.AppendUint32(encoded, skill)
		}
		encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(boss.Metadata)))
		encoded = append(encoded, boss.Metadata...)
	}
	return binary.BigEndian.AppendUint32(encoded, data.Time), nil
}

func appendBossItems(encoded []byte, items []BossItemInfo) []byte {
	encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(items)))
	for _, item := range items {
		encoded = binary.BigEndian.AppendUint32(encoded, item.ItemID)
		encoded = binary.BigEndian.AppendUint16(encoded, item.ItemCount)
		encoded = binary.BigEndian.AppendUint16(encoded, item.DropTime)
	}
	return encoded
}

func ParseCreateNPCBossNetwork(data []byte) (CreateNPCBoss, error) {
	if len(data) < 8 {
		return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS body length %d is too short", len(data))
	}
	count := int(binary.BigEndian.Uint32(data[:4]))
	if count == 0 || count > CompetitiveBossMaxCount {
		return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS boss count %d is outside 1..%d", count, CompetitiveBossMaxCount)
	}
	offset := 4
	result := CreateNPCBoss{Bosses: make([]CompetitiveBossInfo, 0, count)}
	for index := 0; index < count; index++ {
		if len(data)-offset < 28 {
			return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] fixed header needs 28 bytes, only %d remain", index, len(data)-offset)
		}
		boss := CompetitiveBossInfo{
			Label: binary.BigEndian.Uint32(data[offset : offset+4]), BossID: binary.BigEndian.Uint16(data[offset+4 : offset+6]),
			RoleID: data[offset+6], TeamID: data[offset+7], AIType: binary.BigEndian.Uint32(data[offset+8 : offset+12]),
			Row: binary.BigEndian.Uint16(data[offset+12 : offset+14]), Col: binary.BigEndian.Uint16(data[offset+14 : offset+16]),
			HP: binary.BigEndian.Uint16(data[offset+16 : offset+18]), Rate: binary.BigEndian.Uint32(data[offset+18 : offset+22]),
			Bubble: binary.BigEndian.Uint32(data[offset+22 : offset+26]), Power: binary.BigEndian.Uint16(data[offset+26 : offset+28]),
		}
		offset += 28
		var err error
		boss.NormalItems, offset, err = parseBossItems(data, offset, CompetitiveBossMaxItems, index, "normal")
		if err != nil {
			return CreateNPCBoss{}, err
		}
		boss.OutfitItems, offset, err = parseBossItems(data, offset, CompetitiveBossMaxOutfits, index, "outfit")
		if err != nil {
			return CreateNPCBoss{}, err
		}
		if len(data)-offset < 2 {
			return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] lacks skill count", index)
		}
		skillCount := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if skillCount > CompetitiveBossMaxSkills || len(data)-offset < skillCount*4 {
			return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] invalid skill count %d", index, skillCount)
		}
		boss.Skills = make([]uint32, 0, skillCount)
		for skillIndex := 0; skillIndex < skillCount; skillIndex++ {
			boss.Skills = append(boss.Skills, binary.BigEndian.Uint32(data[offset:offset+4]))
			offset += 4
		}
		if len(data)-offset < 4 {
			return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] lacks metadata length", index)
		}
		metadataLength := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if metadataLength > CompetitiveBossMaxMetadata || len(data)-offset < metadataLength {
			return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] invalid metadata length %d", index, metadataLength)
		}
		boss.Metadata = append([]byte(nil), data[offset:offset+metadataLength]...)
		offset += metadataLength
		result.Bosses = append(result.Bosses, boss)
	}
	if len(data)-offset != 4 {
		return CreateNPCBoss{}, fmt.Errorf("CREATE_NPC_BOSS time needs exactly 4 trailing bytes, got %d", len(data)-offset)
	}
	result.Time = binary.BigEndian.Uint32(data[offset : offset+4])
	if err := result.Validate(); err != nil {
		return CreateNPCBoss{}, err
	}
	return result, nil
}

func parseBossItems(data []byte, offset, limit, bossIndex int, kind string) ([]BossItemInfo, int, error) {
	if len(data)-offset < 2 {
		return nil, offset, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] lacks %s item count", bossIndex, kind)
	}
	count := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if count > limit || len(data)-offset < count*8 {
		return nil, offset, fmt.Errorf("CREATE_NPC_BOSS bosses[%d] invalid %s item count %d", bossIndex, kind, count)
	}
	items := make([]BossItemInfo, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, BossItemInfo{
			ItemID: binary.BigEndian.Uint32(data[offset : offset+4]), ItemCount: binary.BigEndian.Uint16(data[offset+4 : offset+6]),
			DropTime: binary.BigEndian.Uint16(data[offset+6 : offset+8]),
		})
		offset += 8
	}
	return items, offset, nil
}

func BuildLocalCreateNPCBossNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data CreateNPCBoss) ([]byte, error) {
	return buildLocalCreateNPCBossNotify(requestPacket, roomID, gameDataSequence, data, nil)
}

func buildLocalCreateNPCBossNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data CreateNPCBoss, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	switch request.Command {
	case StartGameCommand:
	case GameEventRequestCommand:
		event, parseErr := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
		if parseErr != nil {
			return nil, parseErr
		}
		if event.UIN != request.EnvelopeUIN {
			return nil, fmt.Errorf("game-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
		}
	default:
		return nil, fmt.Errorf("CREATE_NPC_BOSS trigger command 0x%04X is not start-game or request-play", request.Command)
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{RoomID: roomID, GameDataSequence: gameDataSequence, Schema: CreateNPCBossEvent, Body: body}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
}
