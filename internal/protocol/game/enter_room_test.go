package game

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

func testEnterRoomPacket(t *testing.T) []byte {
	t.Helper()
	plaintext := []byte{
		0x00, 0x68, 0, 0, 0, 0, 0, 1, 0, 2, 0xff, 0xff, 0, 1,
		0x00, 0x0f, 0x42, 0x41, 0, 0, 0, 1, 0, 9, 7,
	}
	plaintext = append(plaintext, []byte("123456")...)
	plaintext = append(plaintext, make([]byte, 10)...)
	return makeLocalPacketForTest(t, plaintext)
}

func TestDecodeLocalEnterRoomRequest(t *testing.T) {
	request, err := DecodeLocalEnterRoomRequest(testEnterRoomPacket(t))
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.RoomID != 9 || request.RoleID != 7 {
		t.Fatalf("enter-room request = %+v", request)
	}
	var password [16]byte
	copy(password[:], "123456")
	if !request.PasswordMatches(password) {
		t.Fatal("password did not match")
	}
}

func TestBuildLocalEnterRoomPasswordFailure(t *testing.T) {
	packet, err := BuildLocalEnterRoomFailure(testEnterRoomPacket(t), EnterRoomResultPasswordIncorrect, 9)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != EnterRoomCommand || len(inspection.Payload) != enterRoomFailureSize {
		t.Fatalf("enter-room failure = command 0x%04X payload %d", inspection.Command, len(inspection.Payload))
	}
	if got := EnterRoomResultID(binary.BigEndian.Uint16(inspection.Payload[0:2])); got != EnterRoomResultPasswordIncorrect {
		t.Fatalf("enter-room failure result = %d", got)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[2:4]); got != 9 {
		t.Fatalf("enter-room failure room ID = %d", got)
	}
}

func TestEnterRoomResponseMatchesNativeThreePlayerOracle(t *testing.T) {
	response := EnterRoomResponseOld{
		RoomID: 1, LocalTeamID: 1, LocalSeatID: 3, RoomOwnerID: 1,
		SeatStatus: [8]EnterRoomSeatStatus{
			EnterRoomSeatOccupied, EnterRoomSeatOccupied, EnterRoomSeatOccupied,
			EnterRoomSeatOpen, EnterRoomSeatOpen, EnterRoomSeatOpen,
			EnterRoomSeatOpen, EnterRoomSeatOpen,
		},
		GameType: 2,
		Players: []EnterRoomPlayer{
			{Player: PlayerInfoInRoomOld{UIN: 1_000_001, Nickname: "A", PlayerID: 1, TeamID: 1, SeatID: 1, GameInfo: GameInfo{RoleID: 22}}},
			{Player: PlayerInfoInRoomOld{UIN: 1_000_003, Nickname: "C", PlayerID: 3, TeamID: 1, SeatID: 2, GameInfo: GameInfo{RoleID: 23}}},
			{Player: PlayerInfoInRoomOld{UIN: 1_000_002, Nickname: "B", PlayerID: 2, TeamID: 1, SeatID: 3, GameInfo: GameInfo{RoleID: 4}}},
		},
	}
	payload, err := response.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(payload[2:4]) != 1 || payload[4] != 0x13 || payload[10] != 3 {
		t.Fatalf("response header = %x", payload[:19])
	}
	if got, want := len(payload), 482; got != want {
		t.Fatalf("compact response length = %d, want %d", got, want)
	}
	for index, want := range []uint32{1_000_001, 1_000_003, 1_000_002} {
		offset := 19 + index*135
		if got := binary.BigEndian.Uint32(payload[offset : offset+4]); got != want {
			t.Fatalf("player %d UIN = %d at compact offset %d, want %d", index, got, offset, want)
		}
	}
	wantHash, err := hex.DecodeString("8cb0989f342d0e6430cef226e2184ffcc4e842500c992b5a4918fd8eb49701a2")
	if err != nil {
		t.Fatal(err)
	}
	gotHash := sha256.Sum256(payload)
	if !bytes.Equal(gotHash[:], wantHash) {
		t.Fatalf("native three-player payload SHA-256 = %x, want %x", gotHash, wantHash)
	}

	packet, err := BuildLocalEnterRoomSuccessWithReader(testEnterRoomPacket(t), response, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := qqtea.Decrypt(packet[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(plaintext[0:2]) != EnterRoomCommand {
		t.Fatalf("response command = 0x%04X", binary.BigEndian.Uint16(plaintext[0:2]))
	}
}

func TestPlayerInfoInRoomCompactsEmbeddedPetSkills(t *testing.T) {
	player := PlayerInfoInRoomOld{
		UIN: 1_000_001, Nickname: "A", PlayerID: 1, TeamID: 1, SeatID: 1,
	}
	binary.BigEndian.PutUint32(player.PetBaseInfo[34:38], 2)
	player.PetBaseInfo[38] = 0xAA
	player.PetBaseInfo[39] = 0xBB
	player.PetBaseInfo[40] = 0xCC // decoded-slot padding; must not reach the wire
	encoded, err := player.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(encoded), 137; got != want {
		t.Fatalf("player with two pet skills length = %d, want %d", got, want)
	}
	if !bytes.Equal(encoded[len(encoded)-2:], []byte{0xAA, 0xBB}) {
		t.Fatalf("compact pet skill tail = %X", encoded[len(encoded)-3:])
	}
	binary.BigEndian.PutUint32(player.PetBaseInfo[34:38], PetSkillsSlotSize+1)
	if _, err := player.MarshalNetworkBinary(); err == nil {
		t.Fatal("oversized embedded pet skill count was accepted")
	}
}

func TestEnterRoomNotificationUsesRecipientEnvelopeAndJoiningPlayerPayload(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.PlayerID = 9
	profile.Nickname = "糖二"
	notification := EnterRoomNotificationOld{
		Player:    PlayerInfoInRoomOldFromProfile(1_000_002, profile, 2, 3, 1),
		RoomID:    7,
		Attach:    PlayerInfoInRoomAttach{Honor: 11, Attach1: 22, Attach2: 33},
		SpouseUIN: 44,
	}
	packet, err := BuildLocalEnterRoomNotificationWithReader(testEnterRoomPacket(t), notification, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != EnterRoomNotifyCommand || inspection.Route != enterRoomRoute || inspection.SectionID != 7 {
		t.Fatalf("notification route = command 0x%04X route %d section %d", inspection.Command, inspection.Route, inspection.SectionID)
	}
	playerPayload, err := notification.Player.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	wantLength := len(playerPayload) + 2 + playerInRoomAttachSize + 4
	if len(inspection.Payload) != wantLength {
		t.Fatalf("notification payload length = %d, want %d", len(inspection.Payload), wantLength)
	}
	if !bytes.Equal(inspection.Payload[:len(playerPayload)], playerPayload) {
		t.Fatal("notification player payload changed")
	}
	offset := len(playerPayload)
	if binary.BigEndian.Uint16(inspection.Payload[offset:offset+2]) != 7 ||
		binary.BigEndian.Uint32(inspection.Payload[offset+2:offset+6]) != 11 ||
		binary.BigEndian.Uint32(inspection.Payload[offset+6:offset+10]) != 22 ||
		binary.BigEndian.Uint32(inspection.Payload[offset+10:offset+14]) != 33 ||
		binary.BigEndian.Uint32(inspection.Payload[offset+14:offset+18]) != 44 {
		t.Fatalf("notification tail = %X", inspection.Payload[offset:])
	}
}

func TestRoomProjectionKeepsLocalRoomControlCardsInNativeCache(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.Inventory = []ItemInfo{
		NewPermanentItemInfo(SinglePlayerBossCardItemID, 1),
		NewPermanentItemInfo(CompetitiveAICardItemID, 1),
		NewPermanentItemInfo(LargeStaminaPotionItemID, 2),
	}
	player := PlayerInfoInRoomOldFromProfile(1_000_001, profile, 1, 1, 0)
	if len(player.Items) != 3 || player.Items[0].ItemID != SinglePlayerBossCardItemID ||
		player.Items[1].ItemID != CompetitiveAICardItemID || player.Items[2].ItemID != LargeStaminaPotionItemID {
		t.Fatalf("room inventory lost local control cards = %+v", player.Items)
	}
}
