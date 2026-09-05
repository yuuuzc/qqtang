package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// CREATE_PVENPC_BOSS is the server-owned PVE drop-inventory table consumed
	// by Client+0x1FCEA5 before the battlefield NPC objects are initialized.
	CreatePVENPCBossEvent = 0x138A
	PVEBossMaxGroups      = 40
	PVEBossMaxNormalItems = 10
)

// BossItemInfo mirrors BOSS_ITEM_INFO in QQTMsgData.bin.
type BossItemInfo struct {
	ItemID    uint32
	ItemCount uint16
	DropTime  uint16
}

// PVEBossInfo mirrors the variable wire portion of PVEBOSS_INFO. BossCount is
// the number of battlefield instances sharing BossID; NormalItems is copied
// into every matching NPC object's local drop inventory.
type PVEBossInfo struct {
	BossID      uint16
	BossCount   byte
	NormalItems []BossItemInfo
}

// CreatePVENPCBoss mirrors CREATE_PVENPC_BOSS (0x138A).
type CreatePVENPCBoss struct {
	Bosses []PVEBossInfo
}

func (data CreatePVENPCBoss) Validate() error {
	if len(data.Bosses) == 0 || len(data.Bosses) > PVEBossMaxGroups {
		return fmt.Errorf("CREATE_PVENPC_BOSS boss count %d is outside 1..%d", len(data.Bosses), PVEBossMaxGroups)
	}
	seenBosses := make(map[uint16]struct{}, len(data.Bosses))
	for groupIndex, boss := range data.Bosses {
		if boss.BossID == 0 || boss.BossCount == 0 {
			return fmt.Errorf("CREATE_PVENPC_BOSS bosses[%d] needs non-zero BossID and BossCount", groupIndex)
		}
		if _, duplicate := seenBosses[boss.BossID]; duplicate {
			return fmt.Errorf("CREATE_PVENPC_BOSS repeats BossID %d", boss.BossID)
		}
		seenBosses[boss.BossID] = struct{}{}
		if len(boss.NormalItems) > PVEBossMaxNormalItems {
			return fmt.Errorf("CREATE_PVENPC_BOSS boss %d normal item count %d exceeds %d", boss.BossID, len(boss.NormalItems), PVEBossMaxNormalItems)
		}
		seenItems := make(map[uint32]struct{}, len(boss.NormalItems))
		for itemIndex, item := range boss.NormalItems {
			if item.ItemID == 0 || item.ItemCount == 0 {
				return fmt.Errorf("CREATE_PVENPC_BOSS boss %d items[%d] needs non-zero ItemID and ItemCount", boss.BossID, itemIndex)
			}
			if _, duplicate := seenItems[item.ItemID]; duplicate {
				return fmt.Errorf("CREATE_PVENPC_BOSS boss %d repeats ItemID %d", boss.BossID, item.ItemID)
			}
			seenItems[item.ItemID] = struct{}{}
		}
	}
	return nil
}

func (data CreatePVENPCBoss) MarshalNetworkBinary() ([]byte, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	encodedSize := 4
	for _, boss := range data.Bosses {
		encodedSize += 5 + 8*len(boss.NormalItems)
	}
	encoded := make([]byte, 0, encodedSize)
	encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(data.Bosses)))
	for _, boss := range data.Bosses {
		encoded = binary.BigEndian.AppendUint16(encoded, boss.BossID)
		encoded = append(encoded, boss.BossCount)
		encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(boss.NormalItems)))
		for _, item := range boss.NormalItems {
			encoded = binary.BigEndian.AppendUint32(encoded, item.ItemID)
			encoded = binary.BigEndian.AppendUint16(encoded, item.ItemCount)
			encoded = binary.BigEndian.AppendUint16(encoded, item.DropTime)
		}
	}
	return encoded, nil
}

func ParseCreatePVENPCBossNetwork(data []byte) (CreatePVENPCBoss, error) {
	if len(data) < 4 {
		return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS body length %d is too short", len(data))
	}
	groupCount := int(binary.BigEndian.Uint32(data[:4]))
	if groupCount == 0 || groupCount > PVEBossMaxGroups {
		return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS boss count %d is outside 1..%d", groupCount, PVEBossMaxGroups)
	}
	offset := 4
	result := CreatePVENPCBoss{Bosses: make([]PVEBossInfo, 0, groupCount)}
	for groupIndex := 0; groupIndex < groupCount; groupIndex++ {
		if len(data)-offset < 5 {
			return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS bosses[%d] header needs 5 bytes, only %d remain", groupIndex, len(data)-offset)
		}
		boss := PVEBossInfo{
			BossID:    binary.BigEndian.Uint16(data[offset : offset+2]),
			BossCount: data[offset+2],
		}
		itemCount := int(binary.BigEndian.Uint16(data[offset+3 : offset+5]))
		offset += 5
		if itemCount > PVEBossMaxNormalItems {
			return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS boss %d normal item count %d exceeds %d", boss.BossID, itemCount, PVEBossMaxNormalItems)
		}
		if len(data)-offset < itemCount*8 {
			return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS boss %d items need %d bytes, only %d remain", boss.BossID, itemCount*8, len(data)-offset)
		}
		boss.NormalItems = make([]BossItemInfo, 0, itemCount)
		for itemIndex := 0; itemIndex < itemCount; itemIndex++ {
			boss.NormalItems = append(boss.NormalItems, BossItemInfo{
				ItemID:    binary.BigEndian.Uint32(data[offset : offset+4]),
				ItemCount: binary.BigEndian.Uint16(data[offset+4 : offset+6]),
				DropTime:  binary.BigEndian.Uint16(data[offset+6 : offset+8]),
			})
			offset += 8
		}
		result.Bosses = append(result.Bosses, boss)
	}
	if offset != len(data) {
		return CreatePVENPCBoss{}, fmt.Errorf("CREATE_PVENPC_BOSS has %d trailing bytes", len(data)-offset)
	}
	if err := result.Validate(); err != nil {
		return CreatePVENPCBoss{}, err
	}
	return result, nil
}

func BuildLocalCreatePVENPCBossNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data CreatePVENPCBoss) ([]byte, error) {
	return buildLocalCreatePVENPCBossNotify(requestPacket, roomID, gameDataSequence, data, nil)
}

func BuildLocalCreatePVENPCBossNotifyWithReader(requestPacket []byte, roomID uint16, gameDataSequence uint32, data CreatePVENPCBoss, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalCreatePVENPCBossNotify(requestPacket, roomID, gameDataSequence, data, entropy)
}

func buildLocalCreatePVENPCBossNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data CreatePVENPCBoss, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	switch request.Command {
	case StartGameCommand:
		// The validated start response and GAME_BEGIN use the same trigger.
	case GameEventRequestCommand:
		event, parseErr := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
		if parseErr != nil {
			return nil, parseErr
		}
		if event.UIN != request.EnvelopeUIN {
			return nil, fmt.Errorf("game-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
		}
	default:
		return nil, fmt.Errorf("CREATE_PVENPC_BOSS trigger command 0x%04X is not start-game or request-play", request.Command)
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: CreatePVENPCBossEvent, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
}
