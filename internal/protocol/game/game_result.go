package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	gameResultMaxPlayers       = 8
	gameResultMaxFields        = 4
	gameResultFixedNetworkSize = 12

	// A controlled live-client differential mapped the first three extension
	// positions. QQTMsgData permits a fourth pair, but the adventure result UI
	// did not render it, so production code must not assign it a meaning.
	GameResultKillFieldIndex   = 0
	GameResultRescueFieldIndex = 1
	GameResultRewardFieldIndex = 2
	// The fourth pair exists on the wire and is available to special rule
	// modes. Current evidence does not prove that it always means bun count, so
	// keep the ordinal generic until a bun-mode result is captured.
	GameResultModeSpecificFieldIndex = 3
	GameResultKnownFieldCount        = 3
)

// GameResultField is one paired FieldValue1/FieldValue2 extension in
// QQT_GAME_RESULT_DATA. The live adventure result UI proves that Value1 is
// the displayed count and Value2 is the displayed score for the first three
// positions (kill, rescue, reward).
type GameResultField struct {
	Count uint32
	Score uint32
}

// AdventureResultStatistics is the named domain view of the three extension
// pairs rendered by the adventure settlement screen.
type AdventureResultStatistics struct {
	KillCount   uint32
	KillScore   uint32
	RescueCount uint32
	RescueScore uint32
	RewardCount uint32
	RewardScore uint32
}

func (statistics AdventureResultStatistics) GameResultFields() []GameResultField {
	fields := make([]GameResultField, GameResultKnownFieldCount)
	fields[GameResultKillFieldIndex] = GameResultField{Count: statistics.KillCount, Score: statistics.KillScore}
	fields[GameResultRescueFieldIndex] = GameResultField{Count: statistics.RescueCount, Score: statistics.RescueScore}
	fields[GameResultRewardFieldIndex] = GameResultField{Count: statistics.RewardCount, Score: statistics.RewardScore}
	return fields
}

// AdventureStatistics returns the proven three-field view. A shorter result
// is a valid legacy packet but does not contain a complete adventure summary.
func (result GameResultData) AdventureStatistics() (AdventureResultStatistics, bool) {
	if len(result.Fields) < GameResultKnownFieldCount {
		return AdventureResultStatistics{}, false
	}
	return AdventureResultStatistics{
		KillCount:   result.Fields[GameResultKillFieldIndex].Count,
		KillScore:   result.Fields[GameResultKillFieldIndex].Score,
		RescueCount: result.Fields[GameResultRescueFieldIndex].Count,
		RescueScore: result.Fields[GameResultRescueFieldIndex].Score,
		RewardCount: result.Fields[GameResultRewardFieldIndex].Count,
		RewardScore: result.Fields[GameResultRewardFieldIndex].Score,
	}, true
}

// ModeSpecificField exposes the protocol's fourth extension pair without
// assigning one global meaning to it. Bun, treasure and Boss modes may render
// this slot differently.
func (result GameResultData) ModeSpecificField() (GameResultField, bool) {
	if len(result.Fields) <= GameResultModeSpecificFieldIndex {
		return GameResultField{}, false
	}
	return result.Fields[GameResultModeSpecificFieldIndex], true
}

// TotalFieldScore is the adventure growth value contributed by this result.
// A controlled client probe proved that every FieldValue2 entry participates
// in the sum, including the fourth entry that the failure UI does not render.
// Keep that entry unnamed, but never discard it while calculating growth.
func (result GameResultData) TotalFieldScore() uint32 {
	var total uint32
	for _, field := range result.Fields {
		if ^uint32(0)-total < field.Score {
			return ^uint32(0)
		}
		total += field.Score
	}
	return total
}

// GameResultData mirrors QQT_GAME_RESULT_DATA serialized by schema 0x0FBB.
// FieldCount is derived from Fields. Remark and extension ordinals remain
// numeric because their complete legacy enumerations are not yet proven.
type GameResultData struct {
	PlayerID uint16
	Result   GameResultCode
	Remark   uint32
	Point    uint32
	Fields   []GameResultField
}

func (result GameResultData) appendNetworkBinary(dst []byte) ([]byte, error) {
	if result.PlayerID == 0 {
		return nil, fmt.Errorf("QQT_GAME_RESULT_DATA player ID must be non-zero")
	}
	if len(result.Fields) > gameResultMaxFields {
		return nil, fmt.Errorf("QQT_GAME_RESULT_DATA field count %d exceeds %d", len(result.Fields), gameResultMaxFields)
	}
	dst = binary.BigEndian.AppendUint16(dst, result.PlayerID)
	dst = append(dst, byte(result.Result))
	dst = binary.BigEndian.AppendUint32(dst, result.Remark)
	dst = binary.BigEndian.AppendUint32(dst, result.Point)
	dst = append(dst, byte(len(result.Fields)))
	for _, field := range result.Fields {
		dst = binary.BigEndian.AppendUint32(dst, field.Count)
	}
	for _, field := range result.Fields {
		dst = binary.BigEndian.AppendUint32(dst, field.Score)
	}
	return dst, nil
}

// GameOverData mirrors NOTIFY_GAME_OVER: Time, a counted
// QQT_GAME_RESULT_DATA array, and GameMode.
type GameOverData struct {
	Time     uint32
	Results  []GameResultData
	GameMode SettlementGameMode
}

func (data GameOverData) MarshalNetworkBinary() ([]byte, error) {
	if len(data.Results) == 0 || len(data.Results) > gameResultMaxPlayers {
		return nil, fmt.Errorf("NOTIFY_GAME_OVER result count %d is outside 1..%d", len(data.Results), gameResultMaxPlayers)
	}
	encoded := make([]byte, 0, 6+len(data.Results)*gameResultFixedNetworkSize)
	encoded = binary.BigEndian.AppendUint32(encoded, data.Time)
	encoded = append(encoded, byte(len(data.Results)))
	for index, result := range data.Results {
		var err error
		encoded, err = result.appendNetworkBinary(encoded)
		if err != nil {
			return nil, fmt.Errorf("game result %d: %w", index, err)
		}
	}
	encoded = append(encoded, byte(data.GameMode))
	return encoded, nil
}

// ParseGameOverData decodes the variable-size 0x0FBB body. QQTMsgData defines
// each result as an 11-byte scalar prefix, FieldCount, every FieldValue1, then
// every FieldValue2. GameMode follows the final result.
func ParseGameOverData(body []byte) (GameOverData, error) {
	if len(body) < 6+gameResultFixedNetworkSize {
		return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER body has %d bytes, want at least %d", len(body), 6+gameResultFixedNetworkSize)
	}
	resultCount := int(body[4])
	if resultCount == 0 || resultCount > gameResultMaxPlayers {
		return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER result count %d is outside 1..%d", resultCount, gameResultMaxPlayers)
	}
	data := GameOverData{
		Time:    binary.BigEndian.Uint32(body[:4]),
		Results: make([]GameResultData, 0, resultCount),
	}
	offset := 5
	for index := 0; index < resultCount; index++ {
		if len(body)-offset < gameResultFixedNetworkSize+1 {
			return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER result %d is truncated at byte %d", index, offset)
		}
		fieldCount := int(body[offset+11])
		if fieldCount > gameResultMaxFields {
			return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER result %d field count %d exceeds %d", index, fieldCount, gameResultMaxFields)
		}
		resultSize := gameResultFixedNetworkSize + fieldCount*8
		if offset+resultSize >= len(body) {
			return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER result %d needs %d bytes at byte %d, body has %d", index, resultSize, offset, len(body))
		}
		result := GameResultData{
			PlayerID: binary.BigEndian.Uint16(body[offset : offset+2]),
			Result:   GameResultCode(body[offset+2]),
			Remark:   binary.BigEndian.Uint32(body[offset+3 : offset+7]),
			Point:    binary.BigEndian.Uint32(body[offset+7 : offset+11]),
			Fields:   make([]GameResultField, fieldCount),
		}
		value1Offset := offset + gameResultFixedNetworkSize
		value2Offset := value1Offset + fieldCount*4
		for fieldIndex := 0; fieldIndex < fieldCount; fieldIndex++ {
			result.Fields[fieldIndex] = GameResultField{
				Count: binary.BigEndian.Uint32(body[value1Offset+fieldIndex*4 : value1Offset+(fieldIndex+1)*4]),
				Score: binary.BigEndian.Uint32(body[value2Offset+fieldIndex*4 : value2Offset+(fieldIndex+1)*4]),
			}
		}
		data.Results = append(data.Results, result)
		offset += resultSize
	}
	if offset+1 != len(body) {
		return GameOverData{}, fmt.Errorf("NOTIFY_GAME_OVER parsed %d bytes before GameMode, body has %d", offset, len(body))
	}
	data.GameMode = SettlementGameMode(body[offset])
	return data, nil
}

// BuildLocalGameOverRelay acknowledges a client GAME_OVER request while using
// the server-validated result body. This is intentionally different from
// BuildLocalGameEventRelay, which echoes the original body byte-for-byte.
// Match-category normalization must therefore happen before calling it.
func BuildLocalGameOverRelay(requestPacket []byte, data GameOverData) ([]byte, GameEvent, error) {
	request, event, err := parseLocalGameOverRequest(requestPacket)
	if err != nil {
		return nil, GameEvent{}, err
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, GameEvent{}, err
	}
	payload, err := MarshalGameEventPayload(event.UIN, event.ClientTag, event.Schema, body)
	if err != nil {
		return nil, GameEvent{}, err
	}
	response, err := buildLocalResponse(requestPacket, request, GameEventRequestCommand, payload, nil)
	if err != nil {
		return nil, GameEvent{}, err
	}
	event.Body = body
	return response, event, nil
}

// BuildLocalGameOverNotification acknowledges the same validated GAME_OVER as
// a direction-correct room notification. Peers must never consume a different
// settlement-mode byte from the sender's correlated response.
func BuildLocalGameOverNotification(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData) ([]byte, GameEvent, error) {
	request, event, err := parseLocalGameOverRequest(requestPacket)
	if err != nil {
		return nil, GameEvent{}, err
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, GameEvent{}, err
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: event.Schema, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, GameEvent{}, err
	}
	notification, err := buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, nil)
	if err != nil {
		return nil, GameEvent{}, err
	}
	event.Body = body
	return notification, event, nil
}

func parseLocalGameOverRequest(requestPacket []byte) (localPacket, GameEvent, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return localPacket{}, GameEvent{}, err
	}
	if request.Command != GameEventRequestCommand {
		return localPacket{}, GameEvent{}, fmt.Errorf("game-over request command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return localPacket{}, GameEvent{}, err
	}
	if event.UIN != request.EnvelopeUIN {
		return localPacket{}, GameEvent{}, fmt.Errorf("game-over UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
	}
	if event.Schema != NotifyGameOverEvent {
		return localPacket{}, GameEvent{}, fmt.Errorf("game-over schema 0x%04X, want 0x%04X", event.Schema, NotifyGameOverEvent)
	}
	return request, event, nil
}

func BuildLocalGameOverNotifyFromDeath(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData) ([]byte, error) {
	return buildLocalGameOverNotifyFromDeath(requestPacket, roomID, gameDataSequence, data, nil)
}

func BuildLocalGameOverNotifyFromDeathWithReader(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameOverNotifyFromDeath(requestPacket, roomID, gameDataSequence, data, entropy)
}

func buildLocalGameOverNotifyFromDeath(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, fmt.Errorf("game-over trigger command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return nil, err
	}
	if event.Schema != NotifyPlayerDieEvent {
		return nil, fmt.Errorf("game-over trigger schema 0x%04X, want death schema 0x%04X", event.Schema, NotifyPlayerDieEvent)
	}
	if event.UIN != request.EnvelopeUIN {
		return nil, fmt.Errorf("death-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: NotifyGameOverEvent, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
}

// BuildLocalGameOverNotifyFromEvent is used when a surviving player reaches
// the final stage doorway. Unlike the death fallback, the trigger is a valid
// REQUEST_GAME_NEXTMAP rather than NOTIFY_PLAYER_DIE.
func BuildLocalGameOverNotifyFromEvent(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData) ([]byte, error) {
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyGameOverEvent, body, nil)
}

// BuildLocalGameOverPushForRecipient creates an unsolicited room-scoped
// GAME_OVER from any authenticated packet template. It is used when a network
// departure, rather than a client game-event request, concludes a round.
func BuildLocalGameOverPushForRecipient(recipientPacket []byte, roomID uint16, gameDataSequence uint32, data GameOverData) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("game-over push requires a non-zero room ID")
	}
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence, Schema: NotifyGameOverEvent, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, recipient, GameEventNotifyCommand, payload,
		localMessageRoute{Route: 3, SectionID: roomID}, nil,
	)
}
