package probe

import (
	"context"
	"encoding/binary"
	"io"
	"path/filepath"
	"testing"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestPasswordProtectedChatRoomCreationKeepsNormalBackgroundPanel(t *testing.T) {
	const uin uint32 = 1_000_001
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := game.DefaultPlayerProfile()
	if err := store.Save(context.Background(), uin, profile); err != nil {
		t.Fatal(err)
	}

	request := weddingRoomCreateRequest(t, uin, game.RoomPropertyPassword|game.RoomPropertyFreeRule, roomstate.GameTypeChat)
	server := &Server{playerStore: store, logWriter: io.Discard}
	session := &connectionSession{UIN: uin, Profile: profile}
	result := server.handleLobbyMessage(ListenerConfig{Response: ResponseConfig{QQTCreateRoomSuccess: true}}, session, "test", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) != 0 {
		t.Fatalf("wedding-room create result = handled:%t response:%d follow-up:%d", result.handled, len(result.response), len(result.followUp))
	}

	createInspection, err := game.InspectLocalPacket(result.response)
	if err != nil {
		t.Fatal(err)
	}
	if createInspection.Command != game.CreateRoomCommand || len(createInspection.Payload) != 4 {
		t.Fatalf("create response = command 0x%04X payload %X", createInspection.Command, createInspection.Payload)
	}

}

func TestChatRoomWithoutPasswordAlsoKeepsNormalBackgroundPanel(t *testing.T) {
	const uin uint32 = 1_000_001
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := game.DefaultPlayerProfile()
	if err := store.Save(context.Background(), uin, profile); err != nil {
		t.Fatal(err)
	}

	request := weddingRoomCreateRequest(t, uin, game.RoomPropertyStandard, roomstate.GameTypeChat)
	server := &Server{playerStore: store, logWriter: io.Discard}
	session := &connectionSession{UIN: uin, Profile: profile}
	result := server.handleLobbyMessage(ListenerConfig{Response: ResponseConfig{QQTCreateRoomSuccess: true}}, session, "test", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) != 0 {
		t.Fatalf("chat-room create result = handled:%t response:%d follow-up:%d", result.handled, len(result.response), len(result.followUp))
	}
}

func TestCompetitiveRoomCreationDoesNotProjectWeddingMode(t *testing.T) {
	const uin uint32 = 1_000_001
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := game.DefaultPlayerProfile()
	if err := store.Save(context.Background(), uin, profile); err != nil {
		t.Fatal(err)
	}

	request := weddingRoomCreateRequest(t, uin, game.RoomPropertyStandard, roomstate.GameTypeCompetitiveNoItem)
	server := &Server{playerStore: store, logWriter: io.Discard}
	session := &connectionSession{UIN: uin, Profile: profile}
	result := server.handleLobbyMessage(ListenerConfig{Response: ResponseConfig{QQTCreateRoomSuccess: true}}, session, "test", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) != 0 {
		t.Fatalf("competitive-room create result = handled:%t response:%d follow-up:%d", result.handled, len(result.response), len(result.followUp))
	}
}

func weddingRoomCreateRequest(t *testing.T, uin uint32, flag game.RoomPropertyFlag, gameType roomstate.GameType) []byte {
	t.Helper()
	payload := make([]byte, 50)
	binary.BigEndian.PutUint32(payload[0:4], uin)
	copy(payload[8:28], []byte("wedding-room"))
	payload[28] = byte(flag)
	if flag.HasPassword() {
		copy(payload[29:45], []byte("123"))
	}
	payload[45] = byte(gameType)
	binary.BigEndian.PutUint32(payload[46:50], 1)
	return testLocalRoutedPacketWithPayload(t, game.CreateRoomCommand, 2, 0xFFFF, 1, uin, payload)
}
