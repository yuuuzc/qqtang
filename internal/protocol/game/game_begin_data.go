package game

import (
	"encoding/binary"
	"fmt"
)

const (
	GameItemTypeNetworkBinarySize     = 4 + 2
	PlayerGameInfoNetworkBinarySize   = 2 + 1 + 1 + 4 + 4 + 1
	GameBeginMapHashSize              = 32
	GameBeginFileHashSize             = 16
	gameBeginHeaderNetworkBinarySize  = 4 + 4 + 4 + 4 + 2 + 1
	gameBeginTrailerNetworkBinarySize = 1 + 1 + GameBeginMapHashSize + 1 + 4 + GameBeginFileHashSize + 4
	gameBeginFixedNetworkBinarySize   = gameBeginHeaderNetworkBinarySize + gameBeginTrailerNetworkBinarySize
	GameBeginMaxPlayerCount           = 8
	PlayerGameInfoMaxNewItemTypes     = 10
	GameBeginMaxItemTypes             = 32
	GameBeginMaxNewItemTypes          = 100
	GameBeginMaxExpandedItems         = 64
	// NoRemoteContinueFileID tells the installed client to use its local
	// Continue.ini and battlefield data instead of entering the removed
	// official continuation-file download path.
	NoRemoteContinueFileID     int32  = -1
	NoRemoteContinueFileIDWire uint32 = 0xFFFFFFFF
)

// GameItemType mirrors the six-byte GAME_ITEM_TYPE used by GAME_BEGIN_DATA.
// Client+0x1E2975 reads ItemID as a DWORD and expands it Quantity times; the
// shipped QQTEncoder probe confirmed the wire order uint32+int16, big-endian.
type GameItemType struct {
	ItemID   uint32 `json:"item_id"`
	Quantity int16  `json:"quantity"`
}

func (item GameItemType) Validate() error {
	if item.ItemID == 0 {
		return fmt.Errorf("GAME_ITEM_TYPE item ID must be non-zero")
	}
	if item.Quantity <= 0 {
		return fmt.Errorf("GAME_ITEM_TYPE item %d quantity %d must be positive", item.ItemID, item.Quantity)
	}
	return nil
}

func (item GameItemType) appendNetworkBinary(dst []byte) ([]byte, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}
	dst = binary.BigEndian.AppendUint32(dst, item.ItemID)
	dst = binary.BigEndian.AppendUint16(dst, uint16(item.Quantity))
	return dst, nil
}

// PlayerGameInfo mirrors the complete variable-length PLAYER_GAME_INFO.
type PlayerGameInfo struct {
	PlayerID  uint16
	RoleID    byte
	TeamID    byte
	DelayTime uint32
	ExtPoint  uint32
	NewItems  []GameItemType
}

func (info PlayerGameInfo) Validate() error {
	if info.PlayerID == 0 {
		return fmt.Errorf("PLAYER_GAME_INFO player ID must be non-zero")
	}
	if info.RoleID == 0 {
		return fmt.Errorf("PLAYER_GAME_INFO role ID must be non-zero")
	}
	if info.TeamID == 0 {
		return fmt.Errorf("PLAYER_GAME_INFO team ID must be non-zero")
	}
	if err := validateGameItemTypes("PLAYER_GAME_INFO NewItems", info.NewItems, PlayerGameInfoMaxNewItemTypes, 0); err != nil {
		return err
	}
	return nil
}

func (info PlayerGameInfo) AppendNetworkBinary(dst []byte) ([]byte, error) {
	if err := info.Validate(); err != nil {
		return nil, err
	}
	dst = binary.BigEndian.AppendUint16(dst, info.PlayerID)
	dst = append(dst, info.RoleID, info.TeamID)
	dst = binary.BigEndian.AppendUint32(dst, info.DelayTime)
	dst = binary.BigEndian.AppendUint32(dst, info.ExtPoint)
	dst = append(dst, byte(len(info.NewItems)))
	for index, item := range info.NewItems {
		var err error
		dst, err = item.appendNetworkBinary(dst)
		if err != nil {
			return nil, fmt.Errorf("PLAYER_GAME_INFO NewItems[%d]: %w", index, err)
		}
	}
	return dst, nil
}

// GameBeginData mirrors the complete GAME_BEGIN_DATA -> NOTIFY_GAME_BEGIN
// field order, including the three controlled GAME_ITEM_TYPE arrays.
type GameBeginData struct {
	GameID             uint32
	MapID              uint32
	SpawnSeed          uint32
	ItemSeed           uint32
	ArbitratorPlayerID uint16
	Players            []PlayerGameInfo
	Items              []GameItemType
	ReportFlag         byte
	MapHash            [GameBeginMapHashSize]byte
	NewItems           []GameItemType
	ContinueID         int32
	FileHash           [GameBeginFileHashSize]byte
	GameTimeMS         uint32
}

// GameBeginOptions contains the server-owned values that vary for each room
// and adventure stage. Keeping them typed prevents a selected map, continuation
// sequence, experience value, or random seed from being hidden in a byte vector.
type GameBeginOptions struct {
	GameID             uint32
	MapID              uint32
	SpawnSeed          uint32
	ItemSeed           uint32
	ArbitratorPlayerID uint16
	ContinueID         int32
	MapHash            [GameBeginMapHashSize]byte
	FileHash           [GameBeginFileHashSize]byte
	GameTimeMS         uint32
	Players            []PlayerGameInfo
	Items              []GameItemType
	NewItems           []GameItemType
}

func NewGameBeginData(options GameBeginOptions) (GameBeginData, error) {
	if len(options.Players) == 0 {
		return GameBeginData{}, fmt.Errorf("game requires at least one player")
	}
	data := GameBeginData{
		GameID:             options.GameID,
		MapID:              options.MapID,
		SpawnSeed:          options.SpawnSeed,
		ItemSeed:           options.ItemSeed,
		ArbitratorPlayerID: options.ArbitratorPlayerID,
		Players:            clonePlayerGameInfos(options.Players),
		Items:              append([]GameItemType(nil), options.Items...),
		MapHash:            options.MapHash,
		NewItems:           append([]GameItemType(nil), options.NewItems...),
		ContinueID:         options.ContinueID,
		FileHash:           options.FileHash,
		GameTimeMS:         options.GameTimeMS,
	}
	if err := data.Validate(); err != nil {
		return GameBeginData{}, err
	}
	return data, nil
}

// NewAdventureGameBeginData remains as a source-compatible domain alias. The
// wire schema is shared by competitive and adventure matches; mode-specific
// rules belong in the caller that prepares GameBeginOptions.
func NewAdventureGameBeginData(options GameBeginOptions) (GameBeginData, error) {
	return NewGameBeginData(options)
}

func (data GameBeginData) Validate() error {
	if data.GameID == 0 || data.MapID == 0 {
		return fmt.Errorf("GAME_BEGIN_DATA game and map IDs must be non-zero")
	}
	if data.ArbitratorPlayerID == 0 {
		return fmt.Errorf("GAME_BEGIN_DATA arbitrator player ID must be non-zero")
	}
	if data.SpawnSeed == 0 || data.ItemSeed == 0 {
		return fmt.Errorf("GAME_BEGIN_DATA random seeds must be non-zero")
	}
	if len(data.Players) == 0 || len(data.Players) > GameBeginMaxPlayerCount {
		return fmt.Errorf("GAME_BEGIN_DATA player count %d is outside 1..%d", len(data.Players), GameBeginMaxPlayerCount)
	}
	arbitratorPresent := false
	for index, player := range data.Players {
		if err := player.Validate(); err != nil {
			return fmt.Errorf("player %d: %w", index, err)
		}
		if player.PlayerID == data.ArbitratorPlayerID {
			arbitratorPresent = true
		}
	}
	if !arbitratorPresent {
		return fmt.Errorf("GAME_BEGIN_DATA arbitrator player ID %d is not present in Players", data.ArbitratorPlayerID)
	}
	if err := validateGameItemTypes("GAME_BEGIN_DATA Items", data.Items, GameBeginMaxItemTypes, GameBeginMaxExpandedItems); err != nil {
		return err
	}
	if err := validateGameItemTypes("GAME_BEGIN_DATA NewItems", data.NewItems, GameBeginMaxNewItemTypes, 0); err != nil {
		return err
	}
	if data.GameTimeMS == 0 {
		return fmt.Errorf("GAME_BEGIN_DATA game time must be non-zero")
	}
	return nil
}

func (data GameBeginData) MarshalNetworkBinary() ([]byte, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, gameBeginFixedNetworkBinarySize+PlayerGameInfoNetworkBinarySize*len(data.Players)+
		GameItemTypeNetworkBinarySize*(len(data.Items)+len(data.NewItems)))
	encoded = binary.BigEndian.AppendUint32(encoded, data.GameID)
	encoded = binary.BigEndian.AppendUint32(encoded, data.MapID)
	encoded = binary.BigEndian.AppendUint32(encoded, data.SpawnSeed)
	encoded = binary.BigEndian.AppendUint32(encoded, data.ItemSeed)
	encoded = binary.BigEndian.AppendUint16(encoded, data.ArbitratorPlayerID)
	encoded = append(encoded, byte(len(data.Players)))
	for _, player := range data.Players {
		var err error
		encoded, err = player.AppendNetworkBinary(encoded)
		if err != nil {
			return nil, err
		}
	}
	encoded = append(encoded, byte(len(data.Items)))
	for index, item := range data.Items {
		var err error
		encoded, err = item.appendNetworkBinary(encoded)
		if err != nil {
			return nil, fmt.Errorf("GAME_BEGIN_DATA Items[%d]: %w", index, err)
		}
	}
	encoded = append(encoded, data.ReportFlag)
	encoded = append(encoded, data.MapHash[:]...)
	encoded = append(encoded, byte(len(data.NewItems)))
	for index, item := range data.NewItems {
		var err error
		encoded, err = item.appendNetworkBinary(encoded)
		if err != nil {
			return nil, fmt.Errorf("GAME_BEGIN_DATA NewItems[%d]: %w", index, err)
		}
	}
	encoded = binary.BigEndian.AppendUint32(encoded, uint32(data.ContinueID))
	encoded = append(encoded, data.FileHash[:]...)
	encoded = binary.BigEndian.AppendUint32(encoded, data.GameTimeMS)
	return encoded, nil
}

func (data GameBeginData) MarshalLengthPrefixedNetworkBinary() ([]byte, error) {
	inner, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	if len(inner) > 0xFFFF {
		return nil, fmt.Errorf("game-begin inner length %d exceeds uint16", len(inner))
	}
	encoded := make([]byte, 0, 2+len(inner))
	encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(inner)))
	encoded = append(encoded, inner...)
	return encoded, nil
}

func ParseLengthPrefixedGameBeginDataNetwork(payload []byte) (GameBeginData, error) {
	if len(payload) < 2 {
		return GameBeginData{}, fmt.Errorf("GAME_BEGIN_DATA payload length %d is too short", len(payload))
	}
	declared := int(binary.BigEndian.Uint16(payload[:2]))
	if declared != len(payload)-2 {
		return GameBeginData{}, fmt.Errorf("GAME_BEGIN_DATA declared length %d, actual %d", declared, len(payload)-2)
	}
	return ParseGameBeginDataNetwork(payload[2:])
}

func ParseGameBeginDataNetwork(data []byte) (GameBeginData, error) {
	reader := gameBeginReader{data: data}
	gameID, err := reader.uint32("GameID")
	if err != nil {
		return GameBeginData{}, err
	}
	mapID, err := reader.uint32("MapID")
	if err != nil {
		return GameBeginData{}, err
	}
	spawnSeed, err := reader.uint32("SpawnSeed")
	if err != nil {
		return GameBeginData{}, err
	}
	itemSeed, err := reader.uint32("ItemSeed")
	if err != nil {
		return GameBeginData{}, err
	}
	arbitratorPlayerID, err := reader.uint16("ArbitratorPlayerID")
	if err != nil {
		return GameBeginData{}, err
	}
	playerCount, err := reader.uint8("PlayerNum")
	if err != nil {
		return GameBeginData{}, err
	}
	if playerCount == 0 || int(playerCount) > GameBeginMaxPlayerCount {
		return GameBeginData{}, fmt.Errorf("GAME_BEGIN_DATA player count %d is outside 1..%d", playerCount, GameBeginMaxPlayerCount)
	}
	players := make([]PlayerGameInfo, 0, playerCount)
	for index := 0; index < int(playerCount); index++ {
		playerID, playerErr := reader.uint16(fmt.Sprintf("Players[%d].PlayerID", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		roleID, playerErr := reader.uint8(fmt.Sprintf("Players[%d].RoleID", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		teamID, playerErr := reader.uint8(fmt.Sprintf("Players[%d].TeamID", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		delayTime, playerErr := reader.uint32(fmt.Sprintf("Players[%d].DelayTime", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		extPoint, playerErr := reader.uint32(fmt.Sprintf("Players[%d].ExtPoint", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		newItemCount, playerErr := reader.uint8(fmt.Sprintf("Players[%d].NewItemCount", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		newItems, playerErr := reader.gameItemTypes(int(newItemCount), PlayerGameInfoMaxNewItemTypes, fmt.Sprintf("Players[%d].NewItems", index))
		if playerErr != nil {
			return GameBeginData{}, playerErr
		}
		players = append(players, PlayerGameInfo{
			PlayerID:  playerID,
			RoleID:    roleID,
			TeamID:    teamID,
			DelayTime: delayTime,
			ExtPoint:  extPoint,
			NewItems:  newItems,
		})
	}
	itemCount, err := reader.uint8("ItemCount")
	if err != nil {
		return GameBeginData{}, err
	}
	items, err := reader.gameItemTypes(int(itemCount), GameBeginMaxItemTypes, "Items")
	if err != nil {
		return GameBeginData{}, err
	}
	reportFlag, err := reader.uint8("ReportFlag")
	if err != nil {
		return GameBeginData{}, err
	}
	mapHash, err := reader.fixed(GameBeginMapHashSize, "MapHash")
	if err != nil {
		return GameBeginData{}, err
	}
	itemNewCount, err := reader.uint8("ItemNewCount")
	if err != nil {
		return GameBeginData{}, err
	}
	newItems, err := reader.gameItemTypes(int(itemNewCount), GameBeginMaxNewItemTypes, "NewItems")
	if err != nil {
		return GameBeginData{}, err
	}
	continueID, err := reader.uint32("ContinueID")
	if err != nil {
		return GameBeginData{}, err
	}
	fileHash, err := reader.fixed(GameBeginFileHashSize, "FileHash")
	if err != nil {
		return GameBeginData{}, err
	}
	gameTimeMS, err := reader.uint32("GameTimeMS")
	if err != nil {
		return GameBeginData{}, err
	}
	if reader.remaining() != 0 {
		return GameBeginData{}, fmt.Errorf("GAME_BEGIN_DATA has %d trailing bytes", reader.remaining())
	}
	result := GameBeginData{
		GameID:             gameID,
		MapID:              mapID,
		SpawnSeed:          spawnSeed,
		ItemSeed:           itemSeed,
		ArbitratorPlayerID: arbitratorPlayerID,
		Players:            players,
		Items:              items,
		ReportFlag:         reportFlag,
		NewItems:           newItems,
		ContinueID:         int32(continueID),
		GameTimeMS:         gameTimeMS,
	}
	copy(result.MapHash[:], mapHash)
	copy(result.FileHash[:], fileHash)
	if err := result.Validate(); err != nil {
		return GameBeginData{}, err
	}
	return result, nil
}

func clonePlayerGameInfos(players []PlayerGameInfo) []PlayerGameInfo {
	cloned := make([]PlayerGameInfo, len(players))
	for index, player := range players {
		cloned[index] = player
		cloned[index].NewItems = append([]GameItemType(nil), player.NewItems...)
	}
	return cloned
}

func validateGameItemTypes(field string, items []GameItemType, maxTypes, maxExpanded int) error {
	if len(items) > maxTypes {
		return fmt.Errorf("%s count %d exceeds %d", field, len(items), maxTypes)
	}
	expanded := 0
	for index, item := range items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("%s[%d]: %w", field, index, err)
		}
		expanded += int(item.Quantity)
		if maxExpanded > 0 && expanded > maxExpanded {
			return fmt.Errorf("%s expanded quantity %d exceeds %d", field, expanded, maxExpanded)
		}
	}
	return nil
}

type gameBeginReader struct {
	data   []byte
	offset int
}

func (reader *gameBeginReader) remaining() int {
	return len(reader.data) - reader.offset
}

func (reader *gameBeginReader) fixed(size int, field string) ([]byte, error) {
	if size < 0 || reader.remaining() < size {
		return nil, fmt.Errorf("GAME_BEGIN_DATA %s needs %d bytes, only %d remain", field, size, reader.remaining())
	}
	value := reader.data[reader.offset : reader.offset+size]
	reader.offset += size
	return value, nil
}

func (reader *gameBeginReader) uint8(field string) (byte, error) {
	data, err := reader.fixed(1, field)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (reader *gameBeginReader) uint16(field string) (uint16, error) {
	data, err := reader.fixed(2, field)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (reader *gameBeginReader) uint32(field string) (uint32, error) {
	data, err := reader.fixed(4, field)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

func (reader *gameBeginReader) gameItemTypes(count, max int, field string) ([]GameItemType, error) {
	if count < 0 || count > max {
		return nil, fmt.Errorf("GAME_BEGIN_DATA %s count %d exceeds %d", field, count, max)
	}
	items := make([]GameItemType, 0, count)
	for index := 0; index < count; index++ {
		itemID, err := reader.uint32(fmt.Sprintf("%s[%d].ItemID", field, index))
		if err != nil {
			return nil, err
		}
		quantity, err := reader.uint16(fmt.Sprintf("%s[%d].Quantity", field, index))
		if err != nil {
			return nil, err
		}
		item := GameItemType{ItemID: itemID, Quantity: int16(quantity)}
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("GAME_BEGIN_DATA %s[%d]: %w", field, index, err)
		}
		items = append(items, item)
	}
	return items, nil
}
