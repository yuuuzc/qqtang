package probe

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"qqtang/internal/protocol/game"
)

func TestRoomOwnerKickHonorsAntiKickCardAndRemovesOrdinaryMember(t *testing.T) {
	server := &Server{logWriter: io.Discard, liveSessions: make(map[net.Conn]*connectionSession)}
	ownerProfile := game.DefaultPlayerProfile()
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile}
	owner.setUIN(owner.UIN)
	if err := server.createSessionRoom(owner, 1, 2); err != nil {
		t.Fatal(err)
	}

	memberProfile := game.DefaultPlayerProfile()
	memberProfile.PlayerID = 2
	memberProfile.Nickname = "protected"
	memberProfile.Inventory = append(memberProfile.Inventory, game.NewPermanentItemInfo(antiKickCardItemID, 1))
	member := &connectionSession{UIN: 1_000_002, Profile: memberProfile}
	member.setUIN(member.UIN)
	if err := server.worldState().EnterLobby(member.UIN, member.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(member.UIN, member.Profile, owner.RoomID, member.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	member.setRoomID(owner.RoomID)
	member.worldUIN = member.UIN
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server.liveSessions[serverSide] = member

	request := kickOffRequestPacket(t, owner, member)
	config := ListenerConfig{Response: ResponseConfig{QQTRoomList: true}}
	rejected := server.handleRoomMessage(config, owner, "owner", request)
	if !rejected.handled || len(rejected.response) == 0 {
		t.Fatalf("protected kick result = %+v", rejected)
	}
	inspection, err := game.InspectLocalPacket(rejected.response)
	if err != nil || binary.BigEndian.Uint16(inspection.Payload) != uint16(game.KickOffPlayerFailed) {
		t.Fatalf("protected kick response = %+v, %v", inspection, err)
	}
	room, active := server.worldState().Room(owner.RoomID)
	if member.RoomID != owner.RoomID || !active || len(room.Snapshot().Members) != 2 {
		t.Fatal("protected member was removed")
	}

	member.Profile.Inventory = nil
	accepted := server.handleRoomMessage(config, owner, "owner", request)
	if !accepted.handled || len(accepted.response) == 0 || len(accepted.followUp) == 0 {
		t.Fatalf("ordinary kick result = %+v", accepted)
	}
	inspection, err = game.InspectLocalPacket(accepted.response)
	if err != nil || binary.BigEndian.Uint16(inspection.Payload) != uint16(game.KickOffPlayerSuccess) {
		t.Fatalf("ordinary kick response = %+v, %v", inspection, err)
	}
	room, active = server.worldState().Room(owner.RoomID)
	if member.RoomID != 0 || !active || len(room.Snapshot().Members) != 1 {
		t.Fatal("ordinary member remained in room")
	}
}

func TestRoomOwnerKickDoesNotRelockDispatchActor(t *testing.T) {
	server := &Server{logWriter: io.Discard, liveSessions: make(map[net.Conn]*connectionSession)}
	owner := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile(), connectionID: "owner"}
	owner.setUIN(owner.UIN)
	if err := server.createSessionRoom(owner, 1, 2); err != nil {
		t.Fatal(err)
	}
	memberProfile := game.DefaultPlayerProfile()
	memberProfile.PlayerID = 2
	member := &connectionSession{UIN: 1_000_002, Profile: memberProfile, connectionID: "member"}
	member.setUIN(member.UIN)
	if err := server.worldState().EnterLobby(member.UIN, member.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(member.UIN, member.Profile, owner.RoomID, member.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	member.setRoomID(owner.RoomID)
	member.worldUIN = member.UIN
	ownerServer, ownerClient := net.Pipe()
	memberServer, memberClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer memberServer.Close()
	defer memberClient.Close()
	server.liveSessions[ownerServer] = owner
	server.liveSessions[memberServer] = member
	request := kickOffRequestPacket(t, owner, member)
	config := ListenerConfig{Response: ResponseConfig{QQTRoomList: true}}

	// The production TCP loop owns actor.mu for the whole dispatch. Target
	// lookup must therefore use the atomic live identity and never walk back
	// into the already locked owner session.
	owner.mu.Lock()
	done := make(chan protocolMessageResult, 1)
	go func() {
		done <- server.handleKickOffPlayerMessage(config, owner, owner.connectionID, request)
	}()
	select {
	case result := <-done:
		owner.mu.Unlock()
		if !result.handled || result.result != "qqt_room_kick_success" {
			t.Fatalf("locked-actor kick result = %+v", result)
		}
	case <-time.After(time.Second):
		owner.mu.Unlock()
		t.Fatal("kick handler relocked the dispatch actor")
	}
}

func kickOffRequestPacket(t *testing.T, owner, target *connectionSession) []byte {
	t.Helper()
	payload := make([]byte, 14)
	binary.BigEndian.PutUint32(payload[0:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:8], 123)
	binary.BigEndian.PutUint16(payload[8:10], target.Profile.PlayerID)
	binary.BigEndian.PutUint32(payload[10:14], target.UIN)
	return testLocalRoutedPacketWithPayload(t, game.KickOffPlayerCommand, 3, 0xFFFF, owner.RoomID, owner.UIN, payload)
}
