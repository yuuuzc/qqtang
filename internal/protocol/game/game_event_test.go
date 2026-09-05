package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestGameEventDirectionCommandsMatchQQTMsgData(t *testing.T) {
	if GameEventRequestCommand != 0x0083 {
		t.Fatalf("ID_CMS_REQUESTPLAY = 0x%04X, want 0x0083", GameEventRequestCommand)
	}
	if GameEventNotifyCommand != 0x0084 {
		t.Fatalf("ID_SMC_NOTIFYGAMEEVENT = 0x%04X, want 0x0084", GameEventNotifyCommand)
	}
}

func makeDeathEventPacket(t *testing.T) ([]byte, []byte) {
	t.Helper()
	body := make([]byte, 11)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint32(body[2:6], 0x000DCB8F)
	binary.BigEndian.PutUint16(body[6:8], 220)
	binary.BigEndian.PutUint16(body[8:10], 300)
	payload, err := marshalGameEventPayload(1_000_001, 0x1128F838, NotifyPlayerDieEvent, body)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := make([]byte, localInnerHeaderSize, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[:2], GameEventRequestCommand)
	plaintext = append(plaintext, payload...)
	return makeLocalPacketForTest(t, plaintext), payload
}

func TestParseCapturedAdventureEventVectors(t *testing.T) {
	tests := []struct {
		name       string
		hexPayload string
		schema     uint16
		playerID   uint16
		nextMapID  uint32
	}{
		{
			name:       "npc death 30033",
			hexPayload: "000f424111d8b347000fA70f0000755100001b77012c00d000",
			schema:     NotifyPlayerDieEvent,
			playerID:   30033,
		},
		{
			name:       "local player death",
			hexPayload: "000f424111d8b347000fA70f000000010000beeb062b00b400",
			schema:     NotifyPlayerDieEvent,
			playerID:   1,
		},
		{
			name:       "magic kingdom one next stage",
			hexPayload: "000f424111d934fa001278110000000100009d2a0000000100000642",
			schema:     RequestGameNextMap,
			playerID:   1,
			nextMapID:  1602,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := hex.DecodeString(test.hexPayload)
			if err != nil {
				t.Fatal(err)
			}
			event, err := ParseGameEventPayload(payload)
			if err != nil {
				t.Fatal(err)
			}
			if event.Schema != test.schema {
				t.Fatalf("event = %+v", event)
			}
			switch test.schema {
			case NotifyPlayerDieEvent:
				death, err := ParsePlayerDeathEvent(event)
				if err != nil {
					t.Fatal(err)
				}
				if death.PlayerID != test.playerID || death.ItemCount != 0 {
					t.Fatalf("death = %+v", death)
				}
			case RequestGameNextMap:
				request, err := ParseGameNextMapRequest(event)
				if err != nil {
					t.Fatal(err)
				}
				if request.PlayerID != test.playerID || request.ContinueID != 1 || request.NextMapID != test.nextMapID {
					t.Fatalf("next-map request = %+v", request)
				}
			}
		})
	}
}

func TestParsePlayerDeathEventItems(t *testing.T) {
	body := make([]byte, 11, 17)
	binary.BigEndian.PutUint16(body[0:2], 1)
	body[10] = 1
	body = binary.BigEndian.AppendUint32(body, 20044)
	body = append(body, 12, 23)
	event := GameEvent{Schema: NotifyPlayerDieEvent, Body: body}
	death, err := ParsePlayerDeathEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(death.Items) != 1 || death.Items[0] != (GameItem{ItemID: 20044, Row: 12, Col: 23}) {
		t.Fatalf("items = %+v", death.Items)
	}
	event.Body = event.Body[:16]
	if _, err := ParsePlayerDeathEvent(event); err == nil {
		t.Fatal("truncated QQT_GAME_ITEM unexpectedly decoded")
	}
}

func TestNotifyGameEventCapturedDeathVector(t *testing.T) {
	requestPayload, err := hex.DecodeString("000f424100790b6d000fa70f000000010000f85b012c00bf00")
	if err != nil {
		t.Fatal(err)
	}
	event, err := ParseGameEventPayload(requestPayload)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := (NotifyGameEvent{
		RoomID: 1, GameDataSequence: 1, Schema: event.Schema, Body: event.Body,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("0001000000010000000fa70f000000010000f85b012c00bf00")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("NOTIFY_GAME_EVENT death vector = %x, want %x", payload, want)
	}
	decoded, err := ParseNotifyGameEventPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.RoomID != 1 || decoded.GameDataSequence != 1 || decoded.Schema != NotifyPlayerDieEvent || !bytes.Equal(decoded.Body, event.Body) {
		t.Fatalf("decoded NOTIFY_GAME_EVENT = %+v", decoded)
	}
}

func TestBuildLocalGameEventRelayDeathVector(t *testing.T) {
	request, wantPayload := makeDeathEventPacket(t)
	response, event, err := BuildLocalGameEventRelayWithReader(request, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if event.Schema != NotifyPlayerDieEvent || event.UIN != 1_000_001 {
		t.Fatalf("event = %+v", event)
	}
	death, err := ParsePlayerDeathEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if death.PlayerID != 1 || death.ClientTime != 0x000DCB8F || death.PosX != 220 || death.PosY != 300 || death.ItemCount != 0 {
		t.Fatalf("death = %+v", death)
	}
	decoded, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command != GameEventRequestCommand || !bytes.Equal(decoded.Payload, wantPayload) {
		t.Fatalf("relay command/payload = 0x%04X/%x", decoded.Command, decoded.Payload)
	}
}

func TestBuildLocalGameEventNotificationDeathVector(t *testing.T) {
	request, requestPayload := makeDeathEventPacket(t)
	notification, event, err := BuildLocalGameEventNotificationWithReader(request, 1, 0x12345678, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if event.Schema != NotifyPlayerDieEvent {
		t.Fatalf("event = %+v", event)
	}
	decoded, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command != GameEventNotifyCommand {
		t.Fatalf("notification command/payload = 0x%04X/%x", decoded.Command, decoded.Payload)
	}
	notify, err := ParseNotifyGameEventPayload(decoded.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if notify.RoomID != 1 || notify.GameDataSequence != 0x12345678 || notify.Schema != NotifyPlayerDieEvent || !bytes.Equal(notify.Body, event.Body) {
		t.Fatalf("NOTIFY_GAME_EVENT = %+v", notify)
	}
	wantPayload := append([]byte{0, 1, 0x12, 0x34, 0x56, 0x78, 0, 0, 0, 15}, requestPayload[10:]...)
	if !bytes.Equal(decoded.Payload, wantPayload) {
		t.Fatalf("NOTIFY_GAME_EVENT payload = %x, want %x", decoded.Payload, wantPayload)
	}
	if got := binary.BigEndian.Uint16(notification[8:10]); got != 0 {
		t.Fatalf("notification route sequence = 0x%04X", got)
	}
	if got, want := binary.BigEndian.Uint32(notification[4:8]), binary.BigEndian.Uint32(request[4:8]); got != want {
		t.Fatalf("notification outer sequence = 0x%08X, want 0x%08X", got, want)
	}
	if got := binary.BigEndian.Uint16(decoded.Plaintext[6:8]); got != 0 {
		t.Fatalf("notification inner sequence = 0x%04X", got)
	}
}

func TestBuildLocalGameEventNotificationTranslatesPlayerUseBombToNotify(t *testing.T) {
	body := make([]byte, PlayerUseBombBodySize)
	binary.BigEndian.PutUint16(body[0:2], 30001)
	binary.BigEndian.PutUint32(body[2:6], 0x10203040)
	binary.BigEndian.PutUint32(body[6:10], 0x15)
	body[10], body[11] = 4, 5
	binary.BigEndian.PutUint16(body[12:14], 8)

	payload, err := marshalGameEventPayload(1_000_001, 0x1128F838, PlayerUseBomb, body)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := make([]byte, localInnerHeaderSize, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[:2], GameEventRequestCommand)
	plaintext = append(plaintext, payload...)
	request := makeLocalPacketForTest(t, plaintext)

	notification, source, err := BuildLocalGameEventNotificationWithReader(request, 1, 7, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if source.Schema != PlayerUseBomb {
		t.Fatalf("source schema = 0x%04X", source.Schema)
	}
	inspection, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	notify, err := ParseNotifyGameEventPayload(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if notify.Schema != NotifyPlayerUseBomb || !bytes.Equal(notify.Body, body) {
		t.Fatalf("peer bubble notify = schema 0x%04X body %x", notify.Schema, notify.Body)
	}
}

func TestBuildLocalArbitratorChangeNotifyUsesNativeSixByteContract(t *testing.T) {
	recipient, _ := makeDeathEventPacket(t)
	packet, err := BuildLocalArbitratorChangeNotify(recipient, 7, 0x12345678, 2)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != GameEventNotifyCommand {
		t.Fatalf("arbitrator notification command = 0x%04X", inspection.Command)
	}
	notify, err := ParseNotifyGameEventPayload(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := []byte{0, 2, 0, 0, 0, 0}
	if notify.RoomID != 7 || notify.GameDataSequence != 0x12345678 || notify.Schema != NotifyChangeArbitrator || !bytes.Equal(notify.Body, wantBody) {
		t.Fatalf("arbitrator notification = %+v body=%x", notify, notify.Body)
	}
	if _, err = BuildLocalArbitratorChangeNotify(recipient, 7, 1, 0); err == nil {
		t.Fatal("zero arbitrator ID unexpectedly encoded")
	}
}

func TestParseGameEventPayloadRejectsLengthAndSchema(t *testing.T) {
	_, payload := makeDeathEventPacket(t)
	badLength := append([]byte(nil), payload...)
	binary.BigEndian.PutUint16(badLength[8:10], 14)
	if _, err := ParseGameEventPayload(badLength); err == nil {
		t.Fatal("expected bad length to fail")
	}
	badSchema := append([]byte(nil), payload...)
	binary.LittleEndian.PutUint32(badSchema[10:14], 0x00017777)
	if _, err := ParseGameEventPayload(badSchema); err == nil {
		t.Fatal("expected unsupported schema to fail")
	}
}

func TestGameNextMapResponseAndNotification(t *testing.T) {
	body := make([]byte, 14)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint32(body[2:6], 1234)
	binary.BigEndian.PutUint32(body[6:10], 1)
	binary.BigEndian.PutUint32(body[10:14], 1602)
	payload, err := MarshalGameEventPayload(1_000_001, 0x11223344, RequestGameNextMap, body)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[:2], GameEventRequestCommand)
	plaintext = append(plaintext, payload...)
	packet := makeLocalPacketForTest(t, plaintext)
	response, request, err := BuildLocalGameNextMapSuccessWithReader(packet, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if request.PlayerID != 1 || request.ContinueID != 1 || request.NextMapID != 1602 {
		t.Fatalf("request = %+v", request)
	}
	decodedResponse, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	responseEventPayload := decodedResponse.Payload
	if got := binary.LittleEndian.Uint16(responseEventPayload[10:12]); got != ResponseGameNextMap {
		t.Fatalf("response schema = 0x%04X", got)
	}
	data := testGameBeginData()
	notify, err := BuildLocalGameNextMapNotifyWithReader(packet, 1, 9, data, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	decodedNotify, err := InspectLocalPacket(notify)
	if err != nil {
		t.Fatal(err)
	}
	if decodedNotify.Command != GameEventNotifyCommand {
		t.Fatalf("next-map notification command = 0x%04X, want 0x%04X", decodedNotify.Command, GameEventNotifyCommand)
	}
	nextNotify, err := ParseNotifyGameEventPayload(decodedNotify.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if nextNotify.RoomID != 1 || nextNotify.GameDataSequence != 9 || nextNotify.Schema != NotifyGameNextMap {
		t.Fatalf("next-map notification = %+v", nextNotify)
	}
	if got := binary.BigEndian.Uint16(notify[8:10]); got != 0 {
		t.Fatalf("notification route sequence = 0x%04X", got)
	}
}
