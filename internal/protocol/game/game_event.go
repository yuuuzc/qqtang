package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	requestPlayHeaderSize     = 10
	notifyGameEventHeaderSize = 10
	gameDataSchemaSize        = 4
	gameEventMaxDataSize      = 4096

	PlayerUseBomb            = 0x0FA3
	NotifyBombExplode        = 0x0FA4
	NotifyPlayerUseBomb      = 0x138B
	PlayerBeExploded         = 0x0FA5
	NotifyPlayerExploded     = 0x0FA6
	NotifyPlayerDieEvent     = 0x0FA7
	RequestKillPlayer        = 0x0FA8
	NotifyPlayerKilled       = 0x0FA9
	RequestSavePlayer        = 0x0FAA
	NotifyPlayerSaved        = 0x0FAB
	RequestGetItem           = 0x0FAC
	NotifyPlayerGetItem      = 0x0FAD
	NotifyDispatchItem       = 0x0FAE
	RequestUseItem           = 0x0FAF
	NotifyPlayerUseItem      = 0x0FB0
	NotifyChangeArbitrator   = 0x0FB1
	RequestMoveMapElement    = 0x0FB2
	NotifyMapElementMoved    = 0x0FB3
	RequestMoveBomb          = 0x0FB4
	NotifyPlayerRelive       = 0x0FBA
	NotifyPlayerAffection    = 0x0FB5
	RequestGetBun            = 0x0FB6
	NotifyPlayerGetBun       = 0x0FB7
	RequestPutBun            = 0x0FB8
	NotifyPlayerPutBun       = 0x0FB9
	NotifyGameOverEvent      = 0x0FBB
	NotifyMapElementExploded = 0x0FBE
	NotifyItemExploded       = 0x0FBF
	NotifyNPCUseSkill        = 0x1169
	NotifyNPCTalk            = 0x116A
	NotifyNPCDropItem        = 0x116B
	RequestInTreasure        = 0x116C
	ResponseInTreasure       = 0x116D
	RequestTreasureItem      = 0x116E
	ResponseTreasureItem     = 0x116F
	NotifyLeaveTreasure      = 0x1170
	NotifyTreasureExpiry     = 0x1171
	NotifyCheckGameTime      = 0x1172
	NotifyGameTime           = 0x1173
	RequestPreparedUseProp   = 0x1174
	NotifyPreparedUseProp    = 0x1175
	NotifyMapElement         = 0x1176
	NotifyGameNextMap        = 0x1177
	RequestGameNextMap       = 0x1178
	ResponseGameNextMap      = 0x1179
	NotifyUseEmotion         = 0x1194
	NotifyRecoverAvatar      = 0x10E0
	NotifyDispatchBomb       = 0x10E1
	RequestEatBomb           = 0x10E2
	NotifyPlayerEatBomb      = 0x10E3
	NotifyPlayerLeave        = 0x10EC
	RequestLaunchMachine     = 0x10EF
	NotifyMachineFree        = 0x10F0
	NotifyMachineAngry       = 0x10F1
	NotifyMachineGrace       = 0x10F2
	NotifyMachineCool        = 0x10F3
	PlayerBeHarmed           = 0x10F4
	RequestCancelUseProp     = 0x10F5
	NotifyCancelUseProp      = 0x10F6
	NotifyPropTrigger        = 0x10F7
	PlayerThrowBomb          = 0x1159
	NotifyProduceItem        = 0x115A
	NotifyWrestleSkill       = 0x115B
	RequestWrestle           = 0x115C
	NotifyMachineMove        = 0x115D
	RequestPushMapElement    = 0x115E
	NotifyPushMapElement     = 0x115F
	NotifyMapElementKnock    = 0x1160
	NotifyMapElementStop     = 0x1161
	NotifyPlayerKnocked      = 0x1162
	RequestDestroyBox        = 0x1163
	PlayerKnocked            = 0x1164
	BoxStopped               = 0x1165
	NotifyPlayerTrapped      = 0x1166
	NotifyFire               = 0x1167
	NotifyGenerateBox        = 0x1168
	RequestTankBaseHP        = 0x15B3
	NotifyPlayerMoveBomb     = 0x139C

	// UnspecifiedNextMapIDWire is emitted by battlefield definitions whose
	// doorway leaves NextMapIndex empty. ContinueID plus the active server route
	// still identifies exactly one next stage.
	UnspecifiedNextMapIDWire uint32 = 0
)

// GameEvent describes REQUEST_PLAY's validated opaque GameData. The first
// four GameData bytes are a native little-endian DWORD schema ID; they are not
// a uint16 schema followed by a second sequence field.
type GameEvent struct {
	UIN       uint32
	ClientTag uint32
	Schema    uint16
	Body      []byte
}

// NotifyGameEvent mirrors schema 0x041B / NOTIFY_GAME_EVENT. Unlike the
// client-to-server REQUEST_PLAY wrapper, the server direction carries the
// room and a 32-bit server event sequence before the opaque GameData bytes.
type NotifyGameEvent struct {
	RoomID           uint16
	GameDataSequence uint32
	Schema           uint16
	Body             []byte
}

type GameNextMapRequest struct {
	PlayerID   uint16
	ClientTime uint32
	ContinueID uint32
	NextMapID  uint32
}

// PlayerDeathEvent mirrors NOTIFY_PLAYER_DIE. Adventure NPC object IDs use the
// same uint16 PlayerID field, so callers must not assume every event is a
// human participant death.
type PlayerDeathEvent struct {
	PlayerID   uint16
	ClientTime uint32
	PosX       uint16
	PosY       uint16
	ItemCount  byte
	Items      []GameItem
}

func (event PlayerDeathEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_PLAYER_DIE player ID must be non-zero")
	}
	if len(event.Items) > gameItemMaxCount {
		return nil, fmt.Errorf("NOTIFY_PLAYER_DIE item count %d exceeds %d", len(event.Items), gameItemMaxCount)
	}
	body := make([]byte, 0, 11+len(event.Items)*gameItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		body = item.appendNetworkBinary(body)
	}
	return body, nil
}

func ParsePlayerDeathEvent(event GameEvent) (PlayerDeathEvent, error) {
	if event.Schema != NotifyPlayerDieEvent {
		return PlayerDeathEvent{}, fmt.Errorf("player-death schema 0x%04X, want 0x%04X", event.Schema, NotifyPlayerDieEvent)
	}
	if len(event.Body) < 11 {
		return PlayerDeathEvent{}, fmt.Errorf("NOTIFY_PLAYER_DIE body length %d, want at least 11", len(event.Body))
	}
	items, err := parseGameItems(event.Body[11:], event.Body[10])
	if err != nil {
		return PlayerDeathEvent{}, fmt.Errorf("NOTIFY_PLAYER_DIE items: %w", err)
	}
	death := PlayerDeathEvent{
		PlayerID:   binary.BigEndian.Uint16(event.Body[0:2]),
		ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		PosX:       binary.BigEndian.Uint16(event.Body[6:8]),
		PosY:       binary.BigEndian.Uint16(event.Body[8:10]),
		ItemCount:  event.Body[10],
		Items:      items,
	}
	if death.PlayerID == 0 {
		return PlayerDeathEvent{}, fmt.Errorf("NOTIFY_PLAYER_DIE player ID must be non-zero")
	}
	return death, nil
}

func ParseGameNextMapRequest(event GameEvent) (GameNextMapRequest, error) {
	if event.Schema != RequestGameNextMap {
		return GameNextMapRequest{}, fmt.Errorf("game-next-map schema 0x%04X, want 0x%04X", event.Schema, RequestGameNextMap)
	}
	if len(event.Body) != 14 {
		return GameNextMapRequest{}, fmt.Errorf("REQUEST_GAME_NEXTMAP body length %d, want 14", len(event.Body))
	}
	request := GameNextMapRequest{
		PlayerID:   binary.BigEndian.Uint16(event.Body[0:2]),
		ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		ContinueID: binary.BigEndian.Uint32(event.Body[6:10]),
		NextMapID:  binary.BigEndian.Uint32(event.Body[10:14]),
	}
	if request.PlayerID == 0 {
		return GameNextMapRequest{}, fmt.Errorf("REQUEST_GAME_NEXTMAP player ID must be non-zero")
	}
	return request, nil
}

func MarshalGameEventPayload(uin, clientTag uint32, schema uint16, body []byte) ([]byte, error) {
	return marshalGameEventPayload(uin, clientTag, schema, body)
}

func marshalGameEventPayload(uin, clientTag uint32, schema uint16, body []byte) ([]byte, error) {
	gameData, err := marshalGameData(schema, body)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, requestPlayHeaderSize, requestPlayHeaderSize+len(gameData))
	binary.BigEndian.PutUint32(payload[0:4], uin)
	binary.BigEndian.PutUint32(payload[4:8], clientTag)
	binary.BigEndian.PutUint16(payload[8:10], uint16(len(gameData)))
	payload = append(payload, gameData...)
	return payload, nil
}

func marshalGameData(schema uint16, body []byte) ([]byte, error) {
	gameDataLength := gameDataSchemaSize + len(body)
	if gameDataLength > gameEventMaxDataSize {
		return nil, fmt.Errorf("game-event data length %d exceeds %d", gameDataLength, gameEventMaxDataSize)
	}
	gameData := make([]byte, gameDataSchemaSize, gameDataLength)
	binary.LittleEndian.PutUint32(gameData, uint32(schema))
	return append(gameData, body...), nil
}

// MarshalNetworkBinary encodes the direction-specific 0x041B server wrapper.
func (event NotifyGameEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.RoomID == 0 {
		return nil, fmt.Errorf("NOTIFY_GAME_EVENT room ID must be non-zero")
	}
	if event.GameDataSequence == 0 {
		return nil, fmt.Errorf("NOTIFY_GAME_EVENT data sequence must be non-zero")
	}
	gameData, err := marshalGameData(event.Schema, event.Body)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, notifyGameEventHeaderSize, notifyGameEventHeaderSize+len(gameData))
	binary.BigEndian.PutUint16(payload[0:2], event.RoomID)
	binary.BigEndian.PutUint32(payload[2:6], event.GameDataSequence)
	binary.BigEndian.PutUint32(payload[6:10], uint32(len(gameData)))
	return append(payload, gameData...), nil
}

// ParseNotifyGameEventPayload validates schema 0x041B's network payload.
func ParseNotifyGameEventPayload(payload []byte) (NotifyGameEvent, error) {
	if len(payload) < notifyGameEventHeaderSize+gameDataSchemaSize {
		return NotifyGameEvent{}, fmt.Errorf("NOTIFY_GAME_EVENT payload length %d is too short", len(payload))
	}
	gameDataLength := int(binary.BigEndian.Uint32(payload[6:10]))
	if gameDataLength < gameDataSchemaSize || gameDataLength > gameEventMaxDataSize {
		return NotifyGameEvent{}, fmt.Errorf("NOTIFY_GAME_EVENT data length %d is outside %d..%d", gameDataLength, gameDataSchemaSize, gameEventMaxDataSize)
	}
	if len(payload) != notifyGameEventHeaderSize+gameDataLength {
		return NotifyGameEvent{}, fmt.Errorf("NOTIFY_GAME_EVENT data length %d != payload remainder %d", gameDataLength, len(payload)-notifyGameEventHeaderSize)
	}
	roomID := binary.BigEndian.Uint16(payload[0:2])
	sequence := binary.BigEndian.Uint32(payload[2:6])
	if roomID == 0 || sequence == 0 {
		return NotifyGameEvent{}, fmt.Errorf("NOTIFY_GAME_EVENT room/sequence %d/%d must be non-zero", roomID, sequence)
	}
	schemaValue := binary.LittleEndian.Uint32(payload[10:14])
	if schemaValue > 0xffff || !supportedClientBattleEvent(uint16(schemaValue)) {
		return NotifyGameEvent{}, fmt.Errorf("NOTIFY_GAME_EVENT schema 0x%08X is not in the local battle allowlist", schemaValue)
	}
	return NotifyGameEvent{
		RoomID: roomID, GameDataSequence: sequence, Schema: uint16(schemaValue),
		Body: append([]byte(nil), payload[14:]...),
	}, nil
}

// ParseGameEventPayload validates the length-delimited QQTEncoder object used
// by the shipped client's battle relay. Schema is little-endian in this inner
// object even though the surrounding network fields are big-endian.
func ParseGameEventPayload(payload []byte) (GameEvent, error) {
	if len(payload) < requestPlayHeaderSize+gameDataSchemaSize {
		return GameEvent{}, fmt.Errorf("game-event payload length %d is too short", len(payload))
	}
	objectLength := int(binary.BigEndian.Uint16(payload[8:10]))
	if objectLength < gameDataSchemaSize || objectLength > gameEventMaxDataSize {
		return GameEvent{}, fmt.Errorf("game-event object length %d is outside %d..%d", objectLength, gameDataSchemaSize, gameEventMaxDataSize)
	}
	if len(payload) != requestPlayHeaderSize+objectLength {
		return GameEvent{}, fmt.Errorf("game-event object length %d != payload remainder %d", objectLength, len(payload)-requestPlayHeaderSize)
	}
	schemaValue := binary.LittleEndian.Uint32(payload[10:14])
	if schemaValue > 0xffff || !supportedClientBattleEvent(uint16(schemaValue)) {
		return GameEvent{}, fmt.Errorf("game-event schema 0x%08X is not in the local battle allowlist", schemaValue)
	}
	return GameEvent{
		UIN: binary.BigEndian.Uint32(payload[:4]), ClientTag: binary.BigEndian.Uint32(payload[4:8]),
		Schema: uint16(schemaValue), Body: append([]byte(nil), payload[14:]...),
	}, nil
}

func supportedClientBattleEvent(schema uint16) bool {
	switch {
	case schema == uint16(RoomPlayerMoveInfoEvent) || schema == uint16(RoomPlayerPutBombEvent):
		return true
	case schema >= 0x0FA2 && schema <= 0x0FBF && schema != 0x0FBC:
		return true
	case schema >= 0x10E0 && schema <= 0x10F7:
		return true
	case schema >= 0x1159 && schema <= 0x1178:
		return true
	case schema == 0x1194:
		return true
	case schema >= 0x1389 && schema <= 0x138C:
		return true
	case schema == 0x139C:
		return true
	case schema == RequestTankBaseHP:
		return true
	default:
		return false
	}
}

// BuildLocalGameEventRelay acknowledges the exact validated battle object as a
// correlated response on the same local session. A correlated response is not
// dispatched as an in-game notification by NetCenter; room fan-out must use
// BuildLocalGameEventNotification separately.
func BuildLocalGameEventRelay(requestPacket []byte) ([]byte, GameEvent, error) {
	return buildLocalGameEventRelay(requestPacket, nil)
}

func BuildLocalGameEventRelayWithReader(requestPacket []byte, entropy io.Reader) ([]byte, GameEvent, error) {
	if entropy == nil {
		return nil, GameEvent{}, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameEventRelay(requestPacket, entropy)
}

func buildLocalGameEventRelay(requestPacket []byte, entropy io.Reader) ([]byte, GameEvent, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, GameEvent{}, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, GameEvent{}, fmt.Errorf("game-event request command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	event, err := ParseGameEventPayload(payload)
	if err != nil {
		return nil, GameEvent{}, err
	}
	if event.UIN != request.EnvelopeUIN {
		return nil, GameEvent{}, fmt.Errorf("game-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
	}
	response, err := buildLocalResponse(requestPacket, request, GameEventRequestCommand, payload, entropy)
	if err != nil {
		return nil, GameEvent{}, err
	}
	return response, event, nil
}

// BuildLocalGameEventNotification translates REQUEST_PLAY into the
// direction-specific 0x041B / NOTIFY_GAME_EVENT wrapper. The local transport
// routing sequences are zero because this is not the correlated response.
func BuildLocalGameEventNotification(requestPacket []byte, roomID uint16, gameDataSequence uint32) ([]byte, GameEvent, error) {
	return buildLocalGameEventNotification(requestPacket, roomID, gameDataSequence, nil)
}

func BuildLocalGameEventNotificationWithReader(requestPacket []byte, roomID uint16, gameDataSequence uint32, entropy io.Reader) ([]byte, GameEvent, error) {
	if entropy == nil {
		return nil, GameEvent{}, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameEventNotification(requestPacket, roomID, gameDataSequence, entropy)
}

func buildLocalGameEventNotification(requestPacket []byte, roomID uint16, gameDataSequence uint32, entropy io.Reader) ([]byte, GameEvent, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, GameEvent{}, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, GameEvent{}, fmt.Errorf("game-event request command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return nil, GameEvent{}, err
	}
	if event.UIN != request.EnvelopeUIN {
		return nil, GameEvent{}, fmt.Errorf("game-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
	}
	// PLAYER_USE_BOMB is the client request form. QQTMsgData defines the
	// byte-identical NOTIFY_PLAYER_USE_BOMB as its server-to-client peer form.
	// Relaying 0x0FA3 unchanged makes the remote client rebuild the bubble from
	// the actor's cached equipment state instead of the transmitted BombID; that
	// is observably wrong for native Boss actors such as Hook and First Mate.
	notifySchema := peerNotifySchema(event.Schema)
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: notifySchema, Body: event.Body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, GameEvent{}, err
	}
	notification, err := buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
	if err != nil {
		return nil, GameEvent{}, err
	}
	return notification, event, nil
}

// peerNotifySchema converts request-only battle schemas to the corresponding
// server notification schema before fan-out. Keep this mapping deliberately
// small: every entry must be backed by a byte-identical QQTMsgData family.
func peerNotifySchema(schema uint16) uint16 {
	if schema == PlayerUseBomb {
		return NotifyPlayerUseBomb
	}
	return schema
}

func BuildLocalGameNextMapSuccess(requestPacket []byte) ([]byte, GameNextMapRequest, error) {
	return buildLocalGameNextMapSuccess(requestPacket, nil)
}

func BuildLocalGameNextMapSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, GameNextMapRequest, error) {
	if entropy == nil {
		return nil, GameNextMapRequest{}, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameNextMapSuccess(requestPacket, entropy)
}

func buildLocalGameNextMapSuccess(requestPacket []byte, entropy io.Reader) ([]byte, GameNextMapRequest, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, GameNextMapRequest{}, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, GameNextMapRequest{}, fmt.Errorf("game-event request command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return nil, GameNextMapRequest{}, err
	}
	nextRequest, err := ParseGameNextMapRequest(event)
	if err != nil {
		return nil, GameNextMapRequest{}, err
	}
	// RESPONSE_GAME_NEXTMAP: ResultID=0, ReasonLen=0.
	payload, err := marshalGameEventPayload(event.UIN, event.ClientTag, ResponseGameNextMap, []byte{0, 0, 0})
	if err != nil {
		return nil, GameNextMapRequest{}, err
	}
	response, err := buildLocalResponse(requestPacket, request, GameEventRequestCommand, payload, entropy)
	return response, nextRequest, err
}

func BuildLocalGameNextMapNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameBeginData) ([]byte, error) {
	return buildLocalGameNextMapNotify(requestPacket, roomID, gameDataSequence, data, nil)
}

func BuildLocalGameNextMapNotifyWithReader(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameBeginData, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameNextMapNotify(requestPacket, roomID, gameDataSequence, data, entropy)
}

func buildLocalGameNextMapNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data GameBeginData, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, fmt.Errorf("next-map trigger command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return nil, err
	}
	if _, err := ParseGameNextMapRequest(event); err != nil {
		return nil, err
	}
	return buildLocalGameNextMapNotification(requestPacket, request, roomID, gameDataSequence, data, entropy)
}

func buildLocalGameNextMapNotification(requestPacket []byte, request localPacket, roomID uint16, gameDataSequence uint32, data GameBeginData, entropy io.Reader) ([]byte, error) {
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: NotifyGameNextMap, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
}

func buildLocalGameEventPush(requestPacket []byte, roomID uint16, gameDataSequence uint32, schema uint16, body []byte, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != GameEventRequestCommand {
		return nil, fmt.Errorf("game-event trigger command 0x%04X, want 0x%04X", request.Command, GameEventRequestCommand)
	}
	event, err := ParseGameEventPayload(request.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return nil, err
	}
	if event.UIN != request.EnvelopeUIN {
		return nil, fmt.Errorf("game-event UIN %d does not match envelope UIN %d", event.UIN, request.EnvelopeUIN)
	}
	payload, err := (NotifyGameEvent{
		RoomID: roomID, GameDataSequence: gameDataSequence,
		Schema: schema, Body: body,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequest(requestPacket, request, GameEventNotifyCommand, payload, entropy)
}

// BuildLocalGameEventPushForRecipient creates an unsolicited match event from
// any recent authenticated packet belonging to the recipient. It is used for
// authoritative events whose trigger is another connection (for example an
// active player leaving the map).
func BuildLocalGameEventPushForRecipient(recipientPacket []byte, roomID uint16, gameDataSequence uint32, schema uint16, body []byte) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("game-event push requires a non-zero room ID")
	}
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	payload, err := (NotifyGameEvent{RoomID: roomID, GameDataSequence: gameDataSequence, Schema: schema, Body: body}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, recipient, GameEventNotifyCommand, payload,
		localMessageRoute{Route: 3, SectionID: roomID}, nil,
	)
}

// BuildLocalArbitratorChangeNotify rebinds the native battle-scene authority
// after the current arbitrator leaves. Client+0x202B03 consumes the leading
// player ID and resolves its live player object; the legacy protocol retains a
// trailing Time DWORD, but that client handler does not read it.
func BuildLocalArbitratorChangeNotify(recipientPacket []byte, roomID uint16, gameDataSequence uint32, arbitratorID uint16) ([]byte, error) {
	if arbitratorID == 0 {
		return nil, fmt.Errorf("arbitrator-change notification requires a non-zero player ID")
	}
	body := make([]byte, 6)
	binary.BigEndian.PutUint16(body[0:2], arbitratorID)
	// Preserve the six-byte native wire contract while avoiding a fabricated
	// client clock value. The shipped handler intentionally ignores Time.
	binary.BigEndian.PutUint32(body[2:6], 0)
	return BuildLocalGameEventPushForRecipient(recipientPacket, roomID, gameDataSequence, NotifyChangeArbitrator, body)
}
