package game

import (
	"encoding/binary"
	"fmt"
)

// LoginResponse is the typed, bound form of RESPONSE_LOGIN schema 0x07D5.
// LocalLoginConfig is embedded only to share the proven field set; there is no
// extra nested object on wire. ResultID and UIN are added when a request is
// bound to a local profile.
type LoginResponse struct {
	ResultID uint16
	UIN      uint32
	LocalLoginConfig
}

func NewLocalLoginResponse(uin uint32, config LocalLoginConfig) LoginResponse {
	config.KeyGameData = append([]byte(nil), config.KeyGameData...)
	config.Items = append([]ItemInfo(nil), config.Items...)
	config.Reason = append([]byte(nil), config.Reason...)
	config.Patterns = append([]PatternPoint(nil), config.Patterns...)
	return LoginResponse{
		ResultID:         0,
		UIN:              uin,
		LocalLoginConfig: config,
	}
}

func (response LoginResponse) Validate() error {
	if response.ResultID != 0 {
		return fmt.Errorf("login response result ID %d is not a success", response.ResultID)
	}
	if response.UIN == 0 {
		return fmt.Errorf("login response UIN must be non-zero")
	}
	if response.PlayerID == 0 || response.SectionID == 0 {
		return fmt.Errorf("login response player and section IDs must be non-zero")
	}
	if len(response.KeyGameData) > LoginResponseKeyGameDataSize {
		return fmt.Errorf("KeyGameData length %d exceeds %d", len(response.KeyGameData), LoginResponseKeyGameDataSize)
	}
	if err := response.GameInfo.ValidateLocalClientBounds(); err != nil {
		return fmt.Errorf("GAME_INFO: %w", err)
	}
	if len(response.Reason) > LoginResponseMaxReasonBytes {
		return fmt.Errorf("Reason length %d exceeds %d", len(response.Reason), LoginResponseMaxReasonBytes)
	}
	if len(response.Items) > MaxItemInfoCount {
		return fmt.Errorf("item count %d exceeds %d", len(response.Items), MaxItemInfoCount)
	}
	for index, item := range response.Items {
		if item.ItemID == 0 {
			return fmt.Errorf("item %d ID must be non-zero", index)
		}
		if !item.Active() {
			return fmt.Errorf("item %d must have non-zero quantity and available period", index)
		}
	}
	if len(response.Patterns) > patternPointMaxCount {
		return fmt.Errorf("pattern point count %d exceeds %d", len(response.Patterns), patternPointMaxCount)
	}
	return nil
}

func (response LoginResponse) MarshalNetworkBinary() ([]byte, error) {
	if err := response.Validate(); err != nil {
		return nil, err
	}
	payload := make([]byte, 0, LoginResponseFixedPayloadSize+ItemInfoBinarySize*len(response.Items)+len(response.Reason))
	payload = binary.BigEndian.AppendUint16(payload, response.ResultID)
	payload = binary.BigEndian.AppendUint16(payload, response.PlayerID)
	payload = binary.BigEndian.AppendUint32(payload, response.UIN)
	payload = binary.BigEndian.AppendUint32(payload, response.Identity)
	payload = binary.BigEndian.AppendUint16(payload, response.SectionID)
	payload = append(payload, byte(len(response.KeyGameData)))
	key := make([]byte, LoginResponseKeyGameDataSize)
	copy(key, response.KeyGameData)
	payload = append(payload, key...)
	payload = binary.BigEndian.AppendUint16(payload, response.NumOfRoom)
	payload = binary.BigEndian.AppendUint16(payload, response.MinRoomID)
	payload = response.GameInfo.AppendNetworkBinary(payload)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(response.Items)))
	for _, item := range response.Items {
		payload = item.AppendNetworkBinary(payload)
	}
	payload = append(payload, response.SectionMode)
	payload = binary.BigEndian.AppendUint32(payload, response.ExtraSectionModeInfo)
	payload = binary.BigEndian.AppendUint32(payload, response.SectionIdentity)
	payload = append(payload, byte(len(response.Reason)))
	payload = append(payload, response.Reason...)
	payload = binary.BigEndian.AppendUint32(payload, response.Honor)
	payload = binary.BigEndian.AppendUint16(payload, response.TaskCount)
	payload = binary.BigEndian.AppendUint16(payload, response.TaskGrade)
	payload = binary.BigEndian.AppendUint16(payload, response.TaskGameCount)
	payload = binary.BigEndian.AppendUint16(payload, response.TaskGameFinished)
	var err error
	payload, err = appendPatternPoints(payload, response.Patterns)
	if err != nil {
		return nil, fmt.Errorf("login response pattern points: %w", err)
	}

	expectedSize := LoginResponseFixedPayloadSize + ItemInfoBinarySize*len(response.Items) + len(response.Reason) + patternPointNetworkSize*len(response.Patterns)
	if len(payload) != expectedSize {
		return nil, fmt.Errorf("login response payload length %d, want %d", len(payload), expectedSize)
	}
	return payload, nil
}

func ParseLoginResponseNetwork(data []byte) (LoginResponse, error) {
	reader := loginPayloadReader{data: data}
	resultID, err := reader.uint16("ResultID")
	if err != nil {
		return LoginResponse{}, err
	}
	playerID, err := reader.uint16("PlayerID")
	if err != nil {
		return LoginResponse{}, err
	}
	uin, err := reader.uint32("UIN")
	if err != nil {
		return LoginResponse{}, err
	}
	identity, err := reader.uint32("Identity")
	if err != nil {
		return LoginResponse{}, err
	}
	sectionID, err := reader.uint16("SectionID")
	if err != nil {
		return LoginResponse{}, err
	}
	keyLength, err := reader.uint8("KeyGameDataLen")
	if err != nil {
		return LoginResponse{}, err
	}
	if int(keyLength) > LoginResponseKeyGameDataSize {
		return LoginResponse{}, fmt.Errorf("KeyGameData length %d exceeds %d", keyLength, LoginResponseKeyGameDataSize)
	}
	keySlot, err := reader.fixed(LoginResponseKeyGameDataSize, "KeyGameData")
	if err != nil {
		return LoginResponse{}, err
	}
	numOfRoom, err := reader.uint16("NumOfRoom")
	if err != nil {
		return LoginResponse{}, err
	}
	minRoomID, err := reader.uint16("MinRoomID")
	if err != nil {
		return LoginResponse{}, err
	}
	gameInfoBytes, err := reader.fixed(GameInfoNetworkBinarySize, "GAME_INFO")
	if err != nil {
		return LoginResponse{}, err
	}
	gameInfo, err := ParseGameInfoNetwork(gameInfoBytes)
	if err != nil {
		return LoginResponse{}, err
	}
	itemCount, err := reader.uint16("ItemCount")
	if err != nil {
		return LoginResponse{}, err
	}
	if int(itemCount) > MaxItemInfoCount {
		return LoginResponse{}, fmt.Errorf("item count %d exceeds %d", itemCount, MaxItemInfoCount)
	}
	items := make([]ItemInfo, 0, itemCount)
	for index := 0; index < int(itemCount); index++ {
		itemBytes, itemErr := reader.fixed(ItemInfoBinarySize, fmt.Sprintf("ITEM_INFO[%d]", index))
		if itemErr != nil {
			return LoginResponse{}, itemErr
		}
		item, itemErr := ParseItemInfoNetwork(itemBytes)
		if itemErr != nil {
			return LoginResponse{}, itemErr
		}
		items = append(items, item)
	}
	sectionMode, err := reader.uint8("SectionMode")
	if err != nil {
		return LoginResponse{}, err
	}
	extraSectionModeInfo, err := reader.uint32("ExtraSectionModeInfo")
	if err != nil {
		return LoginResponse{}, err
	}
	sectionIdentity, err := reader.uint32("SectionIdentity")
	if err != nil {
		return LoginResponse{}, err
	}
	reasonLength, err := reader.uint8("ReasonLen")
	if err != nil {
		return LoginResponse{}, err
	}
	if int(reasonLength) > LoginResponseMaxReasonBytes {
		return LoginResponse{}, fmt.Errorf("Reason length %d exceeds %d", reasonLength, LoginResponseMaxReasonBytes)
	}
	reason, err := reader.fixed(int(reasonLength), "Reason")
	if err != nil {
		return LoginResponse{}, err
	}
	honor, err := reader.uint32("Honor")
	if err != nil {
		return LoginResponse{}, err
	}
	taskCount, err := reader.uint16("TaskCount")
	if err != nil {
		return LoginResponse{}, err
	}
	taskGrade, err := reader.uint16("TaskGrade")
	if err != nil {
		return LoginResponse{}, err
	}
	taskGameCount, err := reader.uint16("TaskGameCount")
	if err != nil {
		return LoginResponse{}, err
	}
	taskGameFinished, err := reader.uint16("TaskGameFinished")
	if err != nil {
		return LoginResponse{}, err
	}
	patternNum, err := reader.uint8("PatternNum")
	if err != nil {
		return LoginResponse{}, err
	}
	if int(patternNum) > patternPointMaxCount {
		return LoginResponse{}, fmt.Errorf("PatternNum %d exceeds %d", patternNum, patternPointMaxCount)
	}
	patterns := make([]PatternPoint, int(patternNum))
	for index := range patterns {
		encoded, pointErr := reader.fixed(patternPointNetworkSize, fmt.Sprintf("PatternPoints[%d]", index))
		if pointErr != nil {
			return LoginResponse{}, pointErr
		}
		patterns[index] = PatternPoint{
			GameMode: PatternGameMode(encoded[0]), PatternPoint: binary.BigEndian.Uint32(encoded[1:5]),
			PatternLevel: binary.BigEndian.Uint16(encoded[5:7]), LevelValue: binary.BigEndian.Uint32(encoded[7:11]),
		}
	}
	if reader.remaining() != 0 {
		return LoginResponse{}, fmt.Errorf("login response has %d trailing bytes", reader.remaining())
	}

	response := LoginResponse{
		ResultID: resultID,
		UIN:      uin,
		LocalLoginConfig: LocalLoginConfig{
			PlayerID:             playerID,
			Identity:             identity,
			SectionID:            sectionID,
			KeyGameData:          append([]byte(nil), keySlot[:keyLength]...),
			NumOfRoom:            numOfRoom,
			MinRoomID:            minRoomID,
			GameInfo:             gameInfo,
			Items:                items,
			SectionMode:          sectionMode,
			ExtraSectionModeInfo: extraSectionModeInfo,
			SectionIdentity:      sectionIdentity,
			Reason:               append([]byte(nil), reason...),
			Honor:                honor,
			TaskCount:            taskCount,
			TaskGrade:            taskGrade,
			TaskGameCount:        taskGameCount,
			TaskGameFinished:     taskGameFinished,
			Patterns:             patterns,
		},
	}
	if err := response.Validate(); err != nil {
		return LoginResponse{}, err
	}
	return response, nil
}

type loginPayloadReader struct {
	data   []byte
	offset int
}

func (reader *loginPayloadReader) remaining() int {
	return len(reader.data) - reader.offset
}

func (reader *loginPayloadReader) fixed(size int, field string) ([]byte, error) {
	if size < 0 || reader.remaining() < size {
		return nil, fmt.Errorf("login response %s needs %d bytes, only %d remain", field, size, reader.remaining())
	}
	value := reader.data[reader.offset : reader.offset+size]
	reader.offset += size
	return value, nil
}

func (reader *loginPayloadReader) uint8(field string) (byte, error) {
	data, err := reader.fixed(1, field)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (reader *loginPayloadReader) uint16(field string) (uint16, error) {
	data, err := reader.fixed(2, field)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (reader *loginPayloadReader) uint32(field string) (uint32, error) {
	data, err := reader.fixed(4, field)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}
