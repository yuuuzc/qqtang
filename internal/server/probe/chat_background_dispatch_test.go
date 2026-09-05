package probe

import (
	"encoding/binary"
	"net"
	"testing"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func TestChatBackgroundListChangeBroadcastAndEnterProjection(t *testing.T) {
	const ownerUIN uint32 = 1_000_001
	const peerUIN uint32 = 1_000_002
	const roomID uint16 = 12

	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	owner := newChatSession(ownerUIN, 1, 1, 0, ownerServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, ownerUIN))
	peer := newChatSession(peerUIN, 2, 1, 0, peerServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, peerUIN))
	server := newChatTestServer(t, map[net.Conn]*connectionSession{ownerServer: owner, peerServer: peer})
	created, err := server.worldState().CreateRoomWithID(ownerUIN, owner.Profile, roomID, roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeChat,
	}, roomstate.Properties{})
	if err != nil {
		t.Fatal(err)
	}
	owner.setRoomID(roomID)
	if _, err = server.worldState().JoinRoom(peerUIN, peer.Profile, roomID, peer.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(roomID)

	listPayload := binary.BigEndian.AppendUint32(nil, ownerUIN)
	listPayload = binary.BigEndian.AppendUint32(listPayload, 100)
	listPayload = binary.BigEndian.AppendUint32(listPayload, 0)
	listPacket := testLocalRoutedPacketWithPayload(t, game.BackgroundListCommand, 4, 0xffff, roomID, ownerUIN, listPayload)
	listResult := server.handleChatBackgroundMessage(owner, "background-list", listPacket)
	if !listResult.handled || len(listResult.response) == 0 {
		t.Fatalf("background-list result = handled:%t response:%d", listResult.handled, len(listResult.response))
	}
	listInspection, err := game.InspectLocalPacket(listResult.response)
	if err != nil {
		t.Fatal(err)
	}
	if listInspection.Command != game.BackgroundListCommand || binary.BigEndian.Uint32(listInspection.Payload[8:12]) != 10 {
		t.Fatalf("background-list response = %+v payload=%x", listInspection, listInspection.Payload)
	}

	usePayload := binary.BigEndian.AppendUint32(nil, ownerUIN)
	usePayload = binary.BigEndian.AppendUint32(usePayload, 200)
	usePayload = binary.BigEndian.AppendUint32(usePayload, 7)
	usePacket := testLocalRoutedPacketWithPayload(t, game.UseBackgroundCommand, 4, 0xffff, roomID, ownerUIN, usePayload)
	useResult := server.handleChatBackgroundMessage(owner, "background-use", usePacket)
	if !useResult.handled || len(useResult.response) == 0 || len(useResult.followUp) == 0 || useResult.postResponse == nil {
		t.Fatalf("background-use result = handled:%t response:%d follow-up:%d post:%t", useResult.handled, len(useResult.response), len(useResult.followUp), useResult.postResponse != nil)
	}
	weddingInspection, err := game.InspectLocalPacket(useResult.followUp)
	if err != nil {
		t.Fatal(err)
	}
	if weddingInspection.Command != game.ChangeWeddingModeNotifyCommand || binary.BigEndian.Uint16(weddingInspection.Payload[4:6]) != 0 {
		t.Fatalf("western wedding transition = %+v payload=%x", weddingInspection, weddingInspection.Payload)
	}
	peerRead := readChatPacket(peerClient)
	postDone := make(chan struct{})
	go func() {
		useResult.postResponse()
		close(postDone)
	}()
	peerPacket := <-peerRead
	if peerPacket.err != nil {
		t.Fatal(peerPacket.err)
	}
	notification, err := game.InspectLocalPacket(peerPacket.packet)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Command != game.UseBackgroundNotifyCommand || notification.Route != 4 || notification.SectionID != roomID ||
		binary.BigEndian.Uint32(notification.Payload[8:12]) != 7 {
		t.Fatalf("background peer notification = %+v payload=%x", notification, notification.Payload)
	}
	peerWedding := <-readChatPacket(peerClient)
	if peerWedding.err != nil {
		t.Fatal(peerWedding.err)
	}
	peerWeddingInspection, err := game.InspectLocalPacket(peerWedding.packet)
	if err != nil {
		t.Fatal(err)
	}
	if peerWeddingInspection.Command != game.ChangeWeddingModeNotifyCommand || binary.BigEndian.Uint16(peerWeddingInspection.Payload[4:6]) != 0 {
		t.Fatalf("peer western wedding transition = %+v payload=%x", peerWeddingInspection, peerWeddingInspection.Payload)
	}
	<-postDone
	state, ok := server.worldState().Room(roomID)
	if !ok || state.Snapshot().BackgroundID != 7 {
		t.Fatalf("authoritative background state = room:%t snapshot:%+v", ok, state.Snapshot())
	}
	enterResponse, err := server.enterRoomResponse(peer, state.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if enterResponse.BackgroundID != 7 || created.BackgroundID != 0 {
		t.Fatalf("enter background = %d, initial=%d", enterResponse.BackgroundID, created.BackgroundID)
	}
}

func TestChatBackgroundRejectsNonOwnerWithoutBroadcast(t *testing.T) {
	const ownerUIN uint32 = 1_000_001
	const peerUIN uint32 = 1_000_002
	const roomID uint16 = 12
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	owner := newChatSession(ownerUIN, 1, 1, 0, ownerServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, ownerUIN))
	peer := newChatSession(peerUIN, 2, 1, 0, peerServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, peerUIN))
	server := newChatTestServer(t, map[net.Conn]*connectionSession{ownerServer: owner, peerServer: peer})
	if _, err := server.worldState().CreateRoomWithID(ownerUIN, owner.Profile, roomID, roomstate.MatchSettings{Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeChat}, roomstate.Properties{}); err != nil {
		t.Fatal(err)
	}
	owner.setRoomID(roomID)
	if _, err := server.worldState().JoinRoom(peerUIN, peer.Profile, roomID, peer.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(roomID)
	payload := binary.BigEndian.AppendUint32(nil, peerUIN)
	payload = binary.BigEndian.AppendUint32(payload, 200)
	payload = binary.BigEndian.AppendUint32(payload, 7)
	packet := testLocalRoutedPacketWithPayload(t, game.UseBackgroundCommand, 4, 0xffff, roomID, peerUIN, payload)
	result := server.handleChatBackgroundMessage(peer, "background-non-owner", packet)
	if !result.handled || len(result.response) == 0 || result.postResponse != nil {
		t.Fatalf("non-owner result = handled:%t response:%d post:%t", result.handled, len(result.response), result.postResponse != nil)
	}
	inspection, err := game.InspectLocalPacket(result.response)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(inspection.Payload[8:10]) != game.UseBackgroundResultFailed {
		t.Fatalf("non-owner result payload = %x", inspection.Payload)
	}
}

func TestChatBackgroundOwnerAuthorizationTransfersAfterOwnerLeaves(t *testing.T) {
	const ownerUIN uint32 = 1_000_001
	const successorUIN uint32 = 1_000_002
	const roomID uint16 = 12
	ownerServer, ownerClient := net.Pipe()
	successorServer, successorClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer successorServer.Close()
	defer successorClient.Close()
	owner := newChatSession(ownerUIN, 1, 1, 0, ownerServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, ownerUIN))
	successor := newChatSession(successorUIN, 2, 1, 0, successorServer, testLocalRoutedPacket(t, game.PlayerListCommand, 2, 0xffff, 1, successorUIN))
	server := newChatTestServer(t, map[net.Conn]*connectionSession{ownerServer: owner, successorServer: successor})
	if _, err := server.worldState().CreateRoomWithID(ownerUIN, owner.Profile, roomID, roomstate.MatchSettings{Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeChat}, roomstate.Properties{}); err != nil {
		t.Fatal(err)
	}
	owner.setRoomID(roomID)
	if _, err := server.worldState().JoinRoom(successorUIN, successor.Profile, roomID, successor.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	successor.setRoomID(roomID)
	departure, err := server.leaveSessionRoom(owner, roomstate.LeaveVoluntary)
	if err != nil {
		t.Fatal(err)
	}
	if departure.NewOwnerID != successor.Profile.PlayerID {
		t.Fatalf("transferred owner = %d, want %d", departure.NewOwnerID, successor.Profile.PlayerID)
	}
	payload := binary.BigEndian.AppendUint32(nil, successorUIN)
	payload = binary.BigEndian.AppendUint32(payload, 200)
	payload = binary.BigEndian.AppendUint32(payload, 7)
	packet := testLocalRoutedPacketWithPayload(t, game.UseBackgroundCommand, 4, 0xffff, roomID, successorUIN, payload)
	result := server.handleChatBackgroundMessage(successor, "background-new-owner", packet)
	if !result.handled || len(result.response) == 0 || result.postResponse == nil {
		t.Fatalf("new-owner result = handled:%t response:%d post:%t", result.handled, len(result.response), result.postResponse != nil)
	}
	inspection, err := game.InspectLocalPacket(result.response)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[8:10]); got != game.UseBackgroundResultSuccess {
		t.Fatalf("new-owner result = %d, want success", got)
	}
	state, ok := server.worldState().Room(roomID)
	if !ok || state.Snapshot().OwnerID != successor.Profile.PlayerID || state.Snapshot().BackgroundID != 7 {
		t.Fatalf("authoritative transferred room state = room:%t snapshot:%+v", ok, state.Snapshot())
	}
}
