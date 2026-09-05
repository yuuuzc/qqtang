package probe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/game"
)

func TestAdventureLastLivingDepartureSettlesDeadRoomMember(t *testing.T) {
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()

	ownerProfile := game.DefaultPlayerProfile()
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile, connection: ownerServer, connectionID: "owner"}
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, connection: peerServer, connectionID: "peer"}
	owner.setUIN(owner.UIN)
	peer.setUIN(peer.UIN)
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		battles: map[uint32]*match.AdventureBattle{},
		liveSessions: map[net.Conn]*connectionSession{
			ownerServer: owner,
			peerServer:  peer,
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, owner.RoomID, peer.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(owner.RoomID)
	peer.worldUIN = peer.UIN
	if _, err := server.worldState().SetReady(peer.UIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 88
	if _, err := server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	roomID := owner.RoomID
	participants, err := server.localAdventureParticipants(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.replaceAdventureBattle(gameID, 1649, owner.Profile.PlayerID, participants); err != nil {
		t.Fatal(err)
	}
	server.projectRoomMatch(owner.RoomID, gameID, 1649)
	battle, err := server.adventureBattle(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if resolution, deathErr := battle.RecordDeath(peer.Profile.PlayerID); deathErr != nil || resolution.NewlyConcluded {
		t.Fatalf("peer death = %+v err=%v", resolution, deathErr)
	}

	requestPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(requestPayload[0:4], peer.UIN)
	peer.notePacket(testLocalRoutedPacketWithPayload(t, game.LeaveRoomCommand, 3, 0xFFFF, owner.RoomID, peer.UIN, requestPayload))
	packetRead := make(chan []byte, 8)
	readErr := make(chan error, 1)
	go func() { _, _ = io.Copy(io.Discard, ownerClient) }()
	go func() {
		for {
			buffer := make([]byte, 64*1024)
			n, readPacketErr := peerClient.Read(buffer)
			if readPacketErr != nil {
				readErr <- readPacketErr
				return
			}
			packetRead <- append([]byte(nil), buffer[:n]...)
		}
	}()

	owner.mu.Lock()
	server.handleConnectionDeparture(owner, owner.connectionID)
	owner.mu.Unlock()
	var overEvent game.NotifyGameEvent
	seenLeave := false
	deadline := time.After(2 * time.Second)
	for overEvent.Schema == 0 {
		select {
		case packet := <-packetRead:
			inspection, inspectErr := game.InspectLocalPacket(packet)
			if inspectErr != nil {
				t.Fatal(inspectErr)
			}
			switch inspection.Command {
			case game.LeaveRoomNotifyCommand:
				seenLeave = true
			case game.GameEventNotifyCommand:
				event, parseErr := game.ParseNotifyGameEventPayload(inspection.Payload)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				if event.Schema == game.NotifyGameOverEvent {
					if !seenLeave {
						t.Fatal("departure GAME_OVER arrived before NOTIFY_LEAVE_ROOM")
					}
					overEvent = event
				}
			}
		case err = <-readErr:
			t.Fatal(err)
		case <-deadline:
			t.Fatal("dead room member did not receive ordered departure and GAME_OVER")
		}
	}
	over, err := game.ParseGameOverData(overEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if over.GameMode != game.SettlementGameModeAdventure || len(over.Results) != 2 {
		t.Fatalf("departure GAME_OVER = %+v", over)
	}
	for _, result := range over.Results {
		if result.Result != game.GameResultLoss {
			t.Fatalf("departure result = %+v, want loss", result)
		}
	}
	state, active := server.worldState().Room(roomID)
	if !active {
		t.Fatal("room with the dead settled teammate was reclaimed")
	}
	if state.Snapshot().Phase != roomstate.PhasePreparing {
		t.Fatalf("room after departure settlement snapshot=%+v", state.Snapshot())
	}
	if owner.CurrentGameID != 0 || peer.CurrentGameID != 0 {
		t.Fatalf("match projection retained: owner=%d peer=%d", owner.CurrentGameID, peer.CurrentGameID)
	}
	if _, err = server.adventureBattle(gameID); err == nil {
		t.Fatal("concluded adventure battle remained registered")
	}
}

func TestAdventureStageEliminationSettlesBeforeClientConfirmedExitAndKeepsRoom(t *testing.T) {
	eliminatedServer, eliminatedClient := net.Pipe()
	survivorServer, survivorClient := net.Pipe()
	defer eliminatedServer.Close()
	defer eliminatedClient.Close()
	defer survivorServer.Close()
	defer survivorClient.Close()
	go func() { _, _ = io.Copy(io.Discard, survivorClient) }()

	eliminatedProfile := game.DefaultPlayerProfile()
	eliminatedProfile.SectionID = 1
	survivorProfile := game.DefaultPlayerProfile()
	survivorProfile.PlayerID = 2
	survivorProfile.SectionID = 1
	eliminated := &connectionSession{UIN: 1_000_001, Profile: eliminatedProfile, connection: eliminatedServer, connectionID: "eliminated"}
	survivor := &connectionSession{UIN: 1_000_002, Profile: survivorProfile, connection: survivorServer, connectionID: "survivor"}
	eliminated.setUIN(eliminated.UIN)
	survivor.setUIN(survivor.UIN)
	template := testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, eliminated.UIN, make([]byte, 8))
	eliminated.notePacket(template)
	survivor.notePacket(testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, survivor.UIN, make([]byte, 8)))

	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		battles: map[uint32]*match.AdventureBattle{},
		liveSessions: map[net.Conn]*connectionSession{
			eliminatedServer: eliminated,
			survivorServer:   survivor,
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err = server.createSessionRoom(eliminated, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err = server.worldState().EnterLobby(survivor.UIN, survivor.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(survivor.UIN, survivor.Profile, 1, survivor.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	survivor.setRoomID(1)
	survivor.worldUIN = survivor.UIN
	if _, err = server.worldState().SetReady(survivor.UIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 77
	if _, err = server.worldState().StartMatchWithID(eliminated.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []*connectionSession{eliminated, survivor} {
		session.CurrentGameID = gameID
		session.CurrentStageGameID = gameID
		session.CurrentMapID = 1649
	}
	battle, err := match.NewAdventureBattle(gameID, 1649, eliminated.Profile.PlayerID, []match.AdventureParticipant{
		{PlayerID: eliminated.Profile.PlayerID, TeamID: 1},
		{PlayerID: survivor.Profile.PlayerID, TeamID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.battles[gameID] = battle
	if resolution, deathErr := battle.RecordDeath(eliminated.Profile.PlayerID); deathErr != nil || resolution.NewlyConcluded {
		t.Fatalf("eliminated death = %+v err=%v", resolution, deathErr)
	}
	beforeCourage := eliminated.Profile.GameInfo.ExtPoint
	if progress, recordErr := battle.RecordNPCDeath(100); recordErr != nil || !progress.Recorded {
		t.Fatalf("post-death shared NPC courage = %+v err=%v", progress, recordErr)
	}

	commands := make(chan uint16, 8)
	readErr := make(chan error, 1)
	go func() {
		for {
			prefix := make([]byte, 4)
			if _, readPacketErr := io.ReadFull(eliminatedClient, prefix); readPacketErr != nil {
				readErr <- readPacketErr
				return
			}
			packet := make([]byte, binary.BigEndian.Uint32(prefix))
			copy(packet, prefix)
			if _, readPacketErr := io.ReadFull(eliminatedClient, packet[4:]); readPacketErr != nil {
				readErr <- readPacketErr
				return
			}
			inspection, inspectErr := game.InspectLocalPacket(packet)
			if inspectErr != nil {
				readErr <- inspectErr
				return
			}
			commands <- inspection.Command
		}
	}()
	nextMapBody := make([]byte, 14)
	binary.BigEndian.PutUint16(nextMapBody[0:2], survivor.Profile.PlayerID)
	binary.BigEndian.PutUint32(nextMapBody[2:6], 1234)
	triggerPayload, err := game.MarshalGameEventPayload(survivor.UIN, 1, game.RequestGameNextMap, nextMapBody)
	if err != nil {
		t.Fatal(err)
	}
	trigger := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, survivor.UIN, triggerPayload)
	settled := []match.AdventureSettlement{{
		Participant: match.AdventureParticipant{PlayerID: eliminated.Profile.PlayerID, TeamID: 1},
		Reason:      match.AdventureSettlementDiedBeforeNextStage,
	}}
	// REQUEST_GAME_NEXTMAP owns the survivor dispatcher lock. A simultaneous
	// eliminated-client command may own the other session lock, so preparation
	// must not wait for it or the two dispatchers can deadlock in opposite order.
	type preparedResult struct {
		plan adventureStageEliminationPlan
		err  error
	}
	survivor.mu.Lock()
	eliminated.mu.Lock()
	prepared := make(chan preparedResult, 1)
	go func() {
		plan, prepareErr := server.prepareAdventureStageEliminations(trigger, 1, gameID, 1234, survivor.Profile.PlayerID, battle, settled)
		prepared <- preparedResult{plan: plan, err: prepareErr}
	}()
	var plan adventureStageEliminationPlan
	select {
	case result := <-prepared:
		plan, err = result.plan, result.err
	case <-time.After(time.Second):
		t.Fatal("stage-elimination preparation waited for the peer session mutex")
	}
	eliminated.mu.Unlock()
	survivor.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = server.applyAdventureStageEliminations(plan); err != nil {
		t.Fatal(err)
	}

	var ordered []uint16
	deadline := time.After(2 * time.Second)
	for len(ordered) < 2 {
		select {
		case command := <-commands:
			ordered = append(ordered, command)
		case err = <-readErr:
			t.Fatal(err)
		case <-deadline:
			t.Fatalf("eliminated client commands = %x", ordered)
		}
	}
	if ordered[0] != game.GameEventNotifyCommand || ordered[1] != game.RoomPushCommand {
		t.Fatalf("eliminated client command order = %x, want game-over and room-list delta before delayed scene close", ordered)
	}
	if eliminated.RoomID != 0 || eliminated.CurrentGameID != 0 {
		t.Fatalf("eliminated session retained room/game = %d/%d", eliminated.RoomID, eliminated.CurrentGameID)
	}
	if eliminated.detachedRoomID != 1 {
		t.Fatalf("eliminated session detached room = %d, want 1", eliminated.detachedRoomID)
	}
	leavePayload := make([]byte, 8)
	binary.BigEndian.PutUint32(leavePayload[0:4], eliminated.UIN)
	binary.BigEndian.PutUint32(leavePayload[4:8], 5678)
	leaveRequest := testLocalRoutedPacketWithPayload(t, game.LeaveRoomCommand, 3, 0xFFFF, 1, eliminated.UIN, leavePayload)
	leaveResult := server.handleRoomMessage(ListenerConfig{Response: ResponseConfig{QQTRoomList: true}}, eliminated, "eliminated", leaveRequest)
	if !leaveResult.handled || len(leaveResult.response) == 0 || leaveResult.result != "qqt_confirm_detached_room_exit_1" {
		t.Fatalf("detached leave confirmation = %+v", leaveResult)
	}
	if eliminated.detachedRoomID != 0 {
		t.Fatalf("native detached leave did not cancel delayed scene close: %d", eliminated.detachedRoomID)
	}
	leaveInspection, err := game.InspectLocalPacket(leaveResult.response)
	if err != nil {
		t.Fatal(err)
	}
	if leaveInspection.Command != game.LeaveRoomCommand || !bytes.Equal(leaveInspection.Payload, []byte{0, 0}) {
		t.Fatalf("detached leave response = %+v payload=%x", leaveInspection, leaveInspection.Payload)
	}
	if eliminated.Profile.GameInfo.ExtPoint != beforeCourage+70 {
		t.Fatalf("eliminated player courage = %d, want %d shared courage after 30%% loss deduction", eliminated.Profile.GameInfo.ExtPoint, beforeCourage+70)
	}
	state, active := server.worldState().Room(1)
	if !active {
		t.Fatal("survivor room was reclaimed")
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhaseInMatch || len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != survivor.Profile.PlayerID {
		t.Fatalf("survivor room snapshot = %+v", snapshot)
	}
}

func TestAdventureStageDelayedSceneCloseRequiresDetachedState(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	defer serverEnd.Close()
	defer clientEnd.Close()
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{
		UIN: 1_000_001, Profile: profile, connection: serverEnd,
		connectionID: "eliminated", detachedRoomID: 1,
	}
	template := testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, session.UIN, make([]byte, 8))
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{capture: captureWriter, logWriter: io.Discard}
	closed := make(chan bool, 1)
	go func() {
		closed <- server.closeDetachedAdventureStageScene(1, profile.PlayerID, session, template)
	}()
	packet := <-readChatPackets(clientEnd, 1)
	if packet.err != nil {
		t.Fatal(packet.err)
	}
	inspection, err := game.InspectLocalPacket(packet.packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.KickOffRoomNotifyCommand || binary.BigEndian.Uint16(inspection.Payload[0:2]) != game.KickOffRoomReasonCloseGameScene {
		t.Fatalf("delayed scene close = command 0x%04X payload %x", inspection.Command, inspection.Payload)
	}
	closeCompleted := <-closed
	if !closeCompleted || session.detachedRoomID != 0 {
		t.Fatalf("delayed scene close did not consume detached state: closed=%t room=%d", closeCompleted, session.detachedRoomID)
	}
	if server.closeDetachedAdventureStageScene(1, profile.PlayerID, session, template) {
		t.Fatal("completed native scene close was sent twice")
	}
}

func TestAdventureStageEliminationDisconnectedRecipientDoesNotBlockSurvivorProjection(t *testing.T) {
	eliminatedServer, eliminatedClient := net.Pipe()
	survivorServer, survivorClient := net.Pipe()
	defer eliminatedServer.Close()
	defer survivorServer.Close()
	defer survivorClient.Close()

	eliminatedProfile := game.DefaultPlayerProfile()
	eliminatedProfile.SectionID = 1
	survivorProfile := game.DefaultPlayerProfile()
	survivorProfile.PlayerID = 2
	survivorProfile.SectionID = 1
	eliminated := &connectionSession{UIN: 1_000_001, Profile: eliminatedProfile, connection: eliminatedServer, connectionID: "eliminated"}
	survivor := &connectionSession{UIN: 1_000_002, Profile: survivorProfile, connection: survivorServer, connectionID: "survivor"}
	eliminated.setUIN(eliminated.UIN)
	survivor.setUIN(survivor.UIN)
	eliminated.notePacket(testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, eliminated.UIN, make([]byte, 8)))
	survivor.notePacket(testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, survivor.UIN, make([]byte, 8)))

	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		battles: map[uint32]*match.AdventureBattle{},
		liveSessions: map[net.Conn]*connectionSession{
			eliminatedServer: eliminated,
			survivorServer:   survivor,
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err = server.createSessionRoom(eliminated, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err = server.worldState().EnterLobby(survivor.UIN, survivor.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(survivor.UIN, survivor.Profile, 1, survivor.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	survivor.setRoomID(1)
	survivor.worldUIN = survivor.UIN
	if _, err = server.worldState().SetReady(survivor.UIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 78
	if _, err = server.worldState().StartMatchWithID(eliminated.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []*connectionSession{eliminated, survivor} {
		session.CurrentGameID = gameID
		session.CurrentStageGameID = gameID
		session.CurrentMapID = 1649
	}
	participants := []match.AdventureParticipant{
		{PlayerID: eliminated.Profile.PlayerID, TeamID: 1},
		{PlayerID: survivor.Profile.PlayerID, TeamID: 1},
	}
	battle, err := match.NewAdventureBattle(gameID, 1649, eliminated.Profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	server.battles[gameID] = battle
	if resolution, deathErr := battle.RecordDeath(eliminated.Profile.PlayerID); deathErr != nil || resolution.NewlyConcluded {
		t.Fatalf("eliminated death = %+v err=%v", resolution, deathErr)
	}
	nextMapBody := make([]byte, 14)
	binary.BigEndian.PutUint16(nextMapBody[0:2], survivor.Profile.PlayerID)
	binary.BigEndian.PutUint32(nextMapBody[2:6], 1234)
	triggerPayload, err := game.MarshalGameEventPayload(survivor.UIN, 1, game.RequestGameNextMap, nextMapBody)
	if err != nil {
		t.Fatal(err)
	}
	trigger := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, survivor.UIN, triggerPayload)
	plan, err := server.prepareAdventureStageEliminations(trigger, 1, gameID, 1234, survivor.Profile.PlayerID, battle, []match.AdventureSettlement{{
		Participant: participants[0], Reason: match.AdventureSettlementDiedBeforeNextStage,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = eliminatedClient.Close(); err != nil {
		t.Fatal(err)
	}
	survivorReads := readChatPackets(survivorClient, 2)
	if err = server.applyAdventureStageEliminations(plan); err != nil {
		t.Fatal(err)
	}
	for index, command := range []uint16{game.LeaveRoomNotifyCommand, game.RoomPushCommand} {
		got := <-survivorReads
		if got.err != nil {
			t.Fatalf("survivor projection %d: %v", index, got.err)
		}
		inspection, inspectErr := game.InspectLocalPacket(got.packet)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if inspection.Command != command {
			t.Fatalf("survivor projection %d command = 0x%04X, want 0x%04X", index, inspection.Command, command)
		}
	}
	state, active := server.worldState().Room(1)
	if !active || state == nil {
		t.Fatal("survivor room disappeared after disconnected projection")
	}
	snapshot := state.Snapshot()
	if len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != survivor.Profile.PlayerID {
		t.Fatalf("survivor room after disconnected projection = %+v", snapshot)
	}
}

func TestAdventureStageEliminationsProjectOnlyFinalOwner(t *testing.T) {
	serverEnds := make([]net.Conn, 3)
	clientEnds := make([]net.Conn, 3)
	for index := range serverEnds {
		serverEnds[index], clientEnds[index] = net.Pipe()
		defer serverEnds[index].Close()
		defer clientEnds[index].Close()
	}

	sessions := make([]*connectionSession, 3)
	for index := range sessions {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		profile.SectionID = 1
		uin := uint32(1_000_001 + index)
		sessions[index] = &connectionSession{
			UIN: uin, Profile: profile, connection: serverEnds[index], connectionID: fmt.Sprintf("player-%d", index+1),
		}
		sessions[index].setUIN(uin)
		sessions[index].notePacket(testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 2, 0xffff, 1, uin, make([]byte, 8)))
	}
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		battles: map[uint32]*match.AdventureBattle{},
		liveSessions: map[net.Conn]*connectionSession{
			serverEnds[0]: sessions[0], serverEnds[1]: sessions[1], serverEnds[2]: sessions[2],
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err = server.createSessionRoom(sessions[0], 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(sessions); index++ {
		session := sessions[index]
		if err = server.worldState().EnterLobby(session.UIN, session.Profile); err != nil {
			t.Fatal(err)
		}
		if _, err = server.worldState().JoinRoom(session.UIN, session.Profile, 1, session.Profile.GameInfo.RoleID, byte(index)); err != nil {
			t.Fatal(err)
		}
		session.setRoomID(1)
		session.worldUIN = session.UIN
		if _, err = server.worldState().SetReady(session.UIN, true); err != nil {
			t.Fatal(err)
		}
	}
	const gameID uint32 = 91
	if _, err = server.worldState().StartMatchWithID(sessions[0].UIN, gameID); err != nil {
		t.Fatal(err)
	}
	participants := make([]match.AdventureParticipant, 0, len(sessions))
	for _, session := range sessions {
		session.CurrentGameID = gameID
		session.CurrentStageGameID = gameID
		session.CurrentMapID = 1649
		participants = append(participants, match.AdventureParticipant{PlayerID: session.Profile.PlayerID, TeamID: 1})
	}
	battle, err := match.NewAdventureBattle(gameID, 1649, sessions[0].Profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	server.battles[gameID] = battle
	for _, playerID := range []uint16{1, 2} {
		if resolution, deathErr := battle.RecordDeath(playerID); deathErr != nil || resolution.NewlyConcluded {
			t.Fatalf("player %d death = %+v err=%v", playerID, resolution, deathErr)
		}
	}

	nextMapBody := make([]byte, 14)
	binary.BigEndian.PutUint16(nextMapBody[0:2], 3)
	binary.BigEndian.PutUint32(nextMapBody[2:6], 1234)
	triggerPayload, err := game.MarshalGameEventPayload(sessions[2].UIN, 1, game.RequestGameNextMap, nextMapBody)
	if err != nil {
		t.Fatal(err)
	}
	trigger := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, sessions[2].UIN, triggerPayload)
	settled := []match.AdventureSettlement{
		{Participant: participants[0], Reason: match.AdventureSettlementDiedBeforeNextStage},
		{Participant: participants[1], Reason: match.AdventureSettlementDiedBeforeNextStage},
	}
	plan, err := server.prepareAdventureStageEliminations(trigger, 1, gameID, 1234, 3, battle, settled)
	if err != nil {
		t.Fatal(err)
	}
	reads := []<-chan chatPacketRead{
		readChatPackets(clientEnds[0], 2),
		readChatPackets(clientEnds[1], 2),
		readChatPackets(clientEnds[2], 3),
	}
	if err = server.applyAdventureStageEliminations(plan); err != nil {
		t.Fatal(err)
	}
	for recipientIndex := 0; recipientIndex < 2; recipientIndex++ {
		gameOver := <-reads[recipientIndex]
		if gameOver.err != nil {
			t.Fatalf("player %d game-over: %v", recipientIndex+1, gameOver.err)
		}
		gameOverInspection, inspectErr := game.InspectLocalPacket(gameOver.packet)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if gameOverInspection.Command != game.GameEventNotifyCommand {
			t.Fatalf("player %d first command = 0x%04X, want game-over", recipientIndex+1, gameOverInspection.Command)
		}
		roomPush := <-reads[recipientIndex]
		if roomPush.err != nil {
			t.Fatalf("player %d room push: %v", recipientIndex+1, roomPush.err)
		}
		roomPushInspection, inspectErr := game.InspectLocalPacket(roomPush.packet)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if roomPushInspection.Command != game.RoomPushCommand {
			t.Fatalf("player %d final command = 0x%04X, want room-list mutation", recipientIndex+1, roomPushInspection.Command)
		}
	}
	for index := 0; index < 2; index++ {
		got := <-reads[2]
		if got.err != nil {
			t.Fatal(got.err)
		}
		inspection, inspectErr := game.InspectLocalPacket(got.packet)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if inspection.Command != game.LeaveRoomNotifyCommand || binary.BigEndian.Uint16(inspection.Payload[2:4]) != 3 || binary.BigEndian.Uint16(inspection.Payload[4:6]) != 3 {
			t.Fatalf("survivor departure %d = command 0x%04X payload %x", index, inspection.Command, inspection.Payload)
		}
	}
	roomPush := <-reads[2]
	if roomPush.err != nil {
		t.Fatal(roomPush.err)
	}
	roomPushInspection, inspectErr := game.InspectLocalPacket(roomPush.packet)
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	if roomPushInspection.Command != game.RoomPushCommand {
		t.Fatalf("survivor final command = 0x%04X, want room-list mutation", roomPushInspection.Command)
	}
	state, active := server.worldState().Room(1)
	if !active {
		t.Fatal("survivor room was reclaimed")
	}
	snapshot := state.Snapshot()
	if snapshot.OwnerID != 3 || len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != 3 {
		t.Fatalf("final room snapshot = %+v", snapshot)
	}
}
