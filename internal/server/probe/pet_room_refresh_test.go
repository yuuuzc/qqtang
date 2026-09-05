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

func TestCreateRoomRefreshesPetListBeforeRoomResult(t *testing.T) {
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
	pet, err := store.GrantPet(context.Background(), uin, 25_002, "酷比")
	if err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 50)
	binary.BigEndian.PutUint32(payload[0:4], uin)
	copy(payload[8:28], []byte("pet-room"))
	payload[28] = byte(game.RoomPropertyFreeRule)
	payload[45] = byte(roomstate.GameTypeAdventure)
	binary.BigEndian.PutUint32(payload[46:50], 1)
	request := testLocalRoutedPacketWithPayload(t, game.CreateRoomCommand, 2, 0xFFFF, 1, uin, payload)

	server := &Server{playerStore: store, logWriter: io.Discard}
	session := &connectionSession{UIN: uin, Profile: profile}
	result := server.handleLobbyMessage(ListenerConfig{Response: ResponseConfig{QQTCreateRoomSuccess: true}}, session, "test", request)
	if !result.handled || len(result.beforeResponse) == 0 || len(result.response) == 0 || len(result.followUp) != 0 {
		t.Fatalf("create-room result = %+v", result)
	}
	inspection, err := game.InspectLocalPacket(result.beforeResponse)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.PlayerPetsCommand {
		t.Fatalf("create-room pre-response command = 0x%04X, want player-pets 0x%04X", inspection.Command, game.PlayerPetsCommand)
	}
	if len(inspection.Payload) < 2+game.PetInfoWireMinimumSize || binary.BigEndian.Uint32(inspection.Payload[1:5]) != pet.PetID {
		t.Fatalf("create-room pet refresh payload = %X", inspection.Payload)
	}
}
