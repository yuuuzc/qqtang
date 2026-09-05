package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func makeGameOverEventPacket(t *testing.T, data GameOverData) []byte {
	t.Helper()
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalGameEventPayload(1_000_001, 0x11223344, NotifyGameOverEvent, body)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := make([]byte, localInnerHeaderSize, localInnerHeaderSize+len(payload))
	binary.BigEndian.PutUint16(plaintext[:2], GameEventRequestCommand)
	plaintext = append(plaintext, payload...)
	return makeLocalPacketForTest(t, plaintext)
}

func TestGameOverDataMarshalSingleAdventureLoss(t *testing.T) {
	data := GameOverData{
		Time: 0x11223344, GameMode: SettlementGameModeAdventure,
		Results: []GameResultData{{PlayerID: 1, Result: GameResultLoss}},
	}
	got, err := data.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x11, 0x22, 0x33, 0x44, 1,
		0, 1, byte(GameResultLoss),
		0, 0, 0, 0,
		0, 0, 0, 0,
		0,
		byte(SettlementGameModeAdventure),
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("game-over bytes = %x, want %x", got, want)
	}
}

func TestGameOverDataRoundTripsNativeZeroTime(t *testing.T) {
	// Rule 13 (tank) emits its authoritative result table with Time=0 when the
	// final base-destruction condition is met. Time is an opaque client clock,
	// not an object-validity sentinel, so the server must preserve that legal
	// value while normalizing and relaying the result table.
	want := GameOverData{
		Time: 0, GameMode: SettlementGameModeCompetitive,
		Results: []GameResultData{
			{PlayerID: 1, Result: GameResultWin},
			{PlayerID: 2, Result: GameResultLoss},
		},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseGameOverData(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Time != 0 || !bytes.Equal(body[:4], []byte{0, 0, 0, 0}) || len(got.Results) != 2 {
		t.Fatalf("zero-time GAME_OVER round trip = %+v body:%x", got, body)
	}
}

func TestGameOverDataMarshalSingleAdventureWin(t *testing.T) {
	data := GameOverData{
		Time: 0x11223344, GameMode: SettlementGameModeAdventure,
		Results: []GameResultData{{PlayerID: 1, Result: GameResultWin, Point: 500}},
	}
	got, err := data.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x11, 0x22, 0x33, 0x44, 1,
		0, 1, byte(GameResultWin),
		0, 0, 0, 0,
		0, 0, 1, 0xF4,
		0,
		byte(SettlementGameModeAdventure),
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("game-over bytes = %x, want %x", got, want)
	}
}

func TestGameOverDataRoundTripsLegacyModeZeroLayout(t *testing.T) {
	// This captured legacy constructor layout proves that zero is structurally
	// valid. It does not identify zero as the adventure GameMode.
	want := []byte{
		0x00, 0x00, 0x93, 0xD4, 0x01,
		0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00,
		0x00,
	}
	data, err := ParseGameOverData(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := data.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip bytes = %x, want %x", got, want)
	}
}

func TestBuildLocalGameOverResponsesUseValidatedMode(t *testing.T) {
	original := GameOverData{
		Time: 0x11223344, GameMode: SettlementGameModeCompetitive,
		Results: []GameResultData{{PlayerID: 1, Result: GameResultLoss}},
	}
	request := makeGameOverEventPacket(t, original)
	validated := original
	validated.GameMode = SettlementGameModeAdventure

	response, responseEvent, err := BuildLocalGameOverRelay(request, validated)
	if err != nil {
		t.Fatal(err)
	}
	decodedResponse, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	parsedResponse, err := ParseGameEventPayload(decodedResponse.Payload)
	if err != nil {
		t.Fatal(err)
	}
	responseResult, err := ParseGameOverData(parsedResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if responseEvent.Schema != NotifyGameOverEvent || responseResult.GameMode != SettlementGameModeAdventure {
		t.Fatalf("validated response = event %+v result %+v", responseEvent, responseResult)
	}

	notification, notificationEvent, err := BuildLocalGameOverNotification(request, 1, 0x12345678, validated)
	if err != nil {
		t.Fatal(err)
	}
	decodedNotification, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	notify, err := ParseNotifyGameEventPayload(decodedNotification.Payload)
	if err != nil {
		t.Fatal(err)
	}
	notificationResult, err := ParseGameOverData(notify.Body)
	if err != nil {
		t.Fatal(err)
	}
	if notificationEvent.Schema != NotifyGameOverEvent || notificationResult.GameMode != SettlementGameModeAdventure {
		t.Fatalf("validated notification = event %+v result %+v", notificationEvent, notificationResult)
	}
}

func TestParseGameOverDataRejectsMissingFieldCountByte(t *testing.T) {
	truncated := []byte{
		0x00, 0x00, 0x93, 0xD4, 0x01,
		0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00,
	}
	if _, err := ParseGameOverData(truncated); err == nil {
		t.Fatal("accepted 17-byte game-over body without both FieldCount and GameMode")
	}
}

func TestGameOverDataRoundTripsExtensionPairs(t *testing.T) {
	statistics := AdventureResultStatistics{
		KillCount: 1, KillScore: 11,
		RescueCount: 2, RescueScore: 22,
		RewardCount: 3, RewardScore: 33,
	}
	want := GameOverData{
		Time: 0x01020304, GameMode: 0x1A,
		Results: []GameResultData{{
			PlayerID: 1, Result: GameResultWin, Point: 500,
			Fields: statistics.GameResultFields(),
		}},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 42 || body[16] != GameResultKnownFieldCount {
		t.Fatalf("extended body length/count = %d/%d, want 42/3: %x", len(body), body[16], body)
	}
	decoded, err := ParseGameOverData(body)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.Results[0].AdventureStatistics()
	if !ok || got != statistics {
		t.Fatalf("decoded adventure statistics = %+v/%v, want %+v", got, ok, statistics)
	}
}

func TestGameResultPreservesUnnamedModeSpecificField(t *testing.T) {
	result := GameResultData{Fields: []GameResultField{{}, {}, {}, {Count: 7, Score: 70}}}
	field, ok := result.ModeSpecificField()
	if !ok || field.Count != 7 || field.Score != 70 {
		t.Fatalf("mode-specific field = %+v/%t", field, ok)
	}
}

func TestBuildLocalGameOverNotifyFromDeath(t *testing.T) {
	deathPacket, _ := makeDeathEventPacket(t)
	data := GameOverData{
		Time: 0x1128F838, GameMode: SettlementGameModeAdventure,
		Results: []GameResultData{{PlayerID: 1, Result: GameResultLoss}},
	}
	packet, err := BuildLocalGameOverNotifyFromDeathWithReader(deathPacket, 1, 0x23456789, data, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != GameEventNotifyCommand {
		t.Fatalf("game-over notification command = 0x%04X, want 0x%04X", inspection.Command, GameEventNotifyCommand)
	}
	event, err := ParseNotifyGameEventPayload(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if event.Schema != NotifyGameOverEvent {
		t.Fatalf("notification schema = 0x%04X", event.Schema)
	}
	if event.RoomID != 1 || event.GameDataSequence != 0x23456789 {
		t.Fatalf("notification room/sequence = %d/0x%08X", event.RoomID, event.GameDataSequence)
	}
	if binary.BigEndian.Uint32(event.Body[:4]) != data.Time || event.Body[4] != 1 || event.Body[len(event.Body)-1] != byte(SettlementGameModeAdventure) {
		t.Fatalf("notification body = %x", event.Body)
	}
	if len(event.Body) != 18 {
		t.Fatalf("notification body length = %d, want 18", len(event.Body))
	}
	if got := binary.BigEndian.Uint16(packet[8:10]); got != 0 {
		t.Fatalf("notification route sequence = 0x%04X", got)
	}
}

func TestGameModeDomainsRemainDistinct(t *testing.T) {
	if ClientRuleModeAdventure != 0x0C {
		t.Fatalf("adventure client rule mode = %d, want 12", ClientRuleModeAdventure)
	}
	if SettlementGameModeAdventure != 2 {
		t.Fatalf("adventure settlement mode = %d, want PVE protocol category 2", SettlementGameModeAdventure)
	}
	if byte(ClientRuleModeAdventure) == byte(SettlementGameModeAdventure) {
		t.Fatal("client rule and settlement mode domains collapsed")
	}
}
