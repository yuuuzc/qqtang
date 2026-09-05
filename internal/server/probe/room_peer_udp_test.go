package probe

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/game"
)

func TestCompetitiveAIHumanPackagesFromBatchRequiresAuthenticatedSenderIdentity(t *testing.T) {
	batch := game.GameplayBatch{GameID: 77, Entries: []game.GameplayBatchEntry{{
		Index: 3,
		Package: game.GameplayDataPackage{
			PlayerID: 1, Time: 4321, GameID: 77,
			Messages: []game.BattleMessageData{{Time: 4321, DataID: uint32(game.PlayerMoveSchema), Sequence: 9}},
		},
	}}}
	packages, err := competitiveAIHumanPackagesFromBatch(batch, 1)
	if err != nil || len(packages) != 1 || packages[0].PlayerID != 1 || packages[0].GameID != 77 {
		t.Fatalf("validated AI UDP packages = %+v/%v", packages, err)
	}
	batch.Entries[0].Package.PlayerID = 2
	if _, err = competitiveAIHumanPackagesFromBatch(batch, 1); err == nil {
		t.Fatal("batch-authored player mismatch was accepted")
	}
	batch.Entries[0].Package.PlayerID = 1
	batch.Entries[0].Package.GameID = 78
	if _, err = competitiveAIHumanPackagesFromBatch(batch, 1); err == nil {
		t.Fatal("batch/package game mismatch was accepted")
	}
}

func TestRoomPeerMulticastDatagramUsesOnlyOuterFrame(t *testing.T) {
	body, err := (game.PlayerMove{PlayerID: 1, Entries: []game.PlayerMoveEntry{{
		PlayerID: 1,
		Move: game.PlayerMoveSequence{
			Sequence: 3, TimeStamp: 4321, CurrentPosX: 100, CurrentPosY: 120,
			EndPosX: 132, EndPosY: 120, WalkAndDirection: 0x10, Speed: 5,
		},
	}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	build := func(schema uint32) []byte {
		batch, encodeErr := game.EncodeQQTPPPGameplayBatch(game.GameplayBatch{GameID: 77, Entries: []game.GameplayBatchEntry{{
			Index: 9,
			Package: game.GameplayDataPackage{
				PlayerID: 1, Time: 4321, GameID: 77, MessageIndexes: []uint32{9},
				Messages: []game.BattleMessageData{{Time: 4321, DataID: schema, Sequence: 9, Data: body, GameTime: 4321}},
			},
		}}})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		data, encodeErr := (game.LegacyUDPControlPacket{
			Header: game.LegacyUDPControlHeader{PlayerID: 1, UIN: 1_000_001, Type: game.LegacyUDPMulticastType},
			Multicast: &game.LegacyUDPMulticastPayload{
				Targets: []game.LegacyUDPMulticastTarget{{PlayerID: 2, UIN: 1_000_002}}, Data: batch,
			},
		}).Encode()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return data
	}
	if !roomPeerMulticastDatagram(build(uint32(game.PlayerMoveSchema))) {
		t.Fatal("valid movement Type-2 datagram was not classified")
	}
	if !roomPeerMulticastDatagram(build(uint32(game.PlayerUseBomb))) {
		t.Fatal("valid action Type-2 datagram required gameplay-body decoding")
	}
}

func TestRoomPeerUDPRoutesOnlyAfterBothRoomEndpointsRegister(t *testing.T) {
	server, actor, peer := newRoomPeerUDPTestServer(t)
	serverSocket := listenRoomPeerUDPTest(t)
	actorSocket := listenRoomPeerUDPTest(t)
	peerSocket := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer actorSocket.Close()
	defer peerSocket.Close()

	peerPacket := encodeRoomPeerUDPTest(t, peer, actor, uint16(peerSocket.LocalAddr().(*net.UDPAddr).Port))
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "peer-register", serverSocket.LocalAddr().String(), peerSocket.LocalAddr().(*net.UDPAddr), peerPacket) {
		t.Fatal("peer room control packet was not handled")
	}

	actorPort := uint16(actorSocket.LocalAddr().(*net.UDPAddr).Port)
	// The test uses a deliberately untrusted advertised candidate. The relay
	// replaces it with actorPort before delivering it to the target.
	actorPacket := encodeRoomPeerUDPTest(t, actor, peer, uint16(peerSocket.LocalAddr().(*net.UDPAddr).Port))
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "actor-route", serverSocket.LocalAddr().String(), actorSocket.LocalAddr().(*net.UDPAddr), actorPacket) {
		t.Fatal("actor room control packet was not handled")
	}
	if err := peerSocket.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 64)
	n, from, err := peerSocket.ReadFromUDP(got)
	if err != nil {
		t.Fatal(err)
	}
	if from.Port != serverSocket.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("routed datagram source = %s, want server port %d", from, serverSocket.LocalAddr().(*net.UDPAddr).Port)
	}
	actorAddress := actorSocket.LocalAddr().(*net.UDPAddr)
	routed, err := game.DecodeLegacyUDPControlPacket(got[:n])
	if err != nil {
		t.Fatal(err)
	}
	if routed.RoomPeer.Endpoint.IPv4 != [4]byte{127, 0, 0, 1} || routed.RoomPeer.Endpoint.Port != actorPort {
		t.Fatalf("routed source endpoint = %v:%d, want observed %s", routed.RoomPeer.Endpoint.IPv4, routed.RoomPeer.Endpoint.Port, actorAddress)
	}
	if routed.RoomPeer.TargetPlayerID != peer.Profile.PlayerID || routed.RoomPeer.TargetUIN != peer.UIN {
		t.Fatalf("routed target = player %d UIN %d, want player %d UIN %d", routed.RoomPeer.TargetPlayerID, routed.RoomPeer.TargetUIN, peer.Profile.PlayerID, peer.UIN)
	}
}

func TestRoomPeerUDPHandshakeRelaysToAuthenticatedRoomPeers(t *testing.T) {
	server, actor, peer := newRoomPeerUDPTestServer(t)
	serverSocket := listenRoomPeerUDPTest(t)
	actorSocket := listenRoomPeerUDPTest(t)
	peerSocket := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer actorSocket.Close()
	defer peerSocket.Close()

	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: actor.UIN}] = roomPeerUDPEndpoint{
		PlayerID: actor.Profile.PlayerID, Address: actorSocket.LocalAddr().(*net.UDPAddr), Connection: serverSocket,
		Local: serverSocket.LocalAddr().String(), Listener: "game-udp", LastUpdated: time.Now(),
	}
	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: peer.UIN}] = roomPeerUDPEndpoint{
		PlayerID: peer.Profile.PlayerID, Address: peerSocket.LocalAddr().(*net.UDPAddr), Connection: serverSocket,
		Local: serverSocket.LocalAddr().String(), Listener: "game-udp", LastUpdated: time.Now(),
	}
	handshake := game.LegacyUDPPeerHandshake{
		Flags: game.LegacyUDPPeerHandshakeRequest, UIN: actor.UIN,
		Endpoint: game.LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: uint16(serverSocket.LocalAddr().(*net.UDPAddr).Port)},
	}
	data, err := handshake.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "handshake", serverSocket.LocalAddr().String(), actorSocket.LocalAddr().(*net.UDPAddr), data) {
		t.Fatal("peer handshake was not handled")
	}
	if err = peerSocket.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, game.LegacyUDPPeerHandshakeSize)
	n, from, err := peerSocket.ReadFromUDP(got)
	if err != nil {
		t.Fatal(err)
	}
	if from.Port != serverSocket.LocalAddr().(*net.UDPAddr).Port || !bytes.Equal(got[:n], data) {
		t.Fatalf("relayed handshake from=%s data=%x, want server source and %x", from, got[:n], data)
	}
}

func TestRoomPeerUDPHandshakeRejectsUnregisteredSourceEndpoint(t *testing.T) {
	server, actor, peer := newRoomPeerUDPTestServer(t)
	serverSocket := listenRoomPeerUDPTest(t)
	registeredSocket := listenRoomPeerUDPTest(t)
	spoofSocket := listenRoomPeerUDPTest(t)
	peerSocket := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer registeredSocket.Close()
	defer spoofSocket.Close()
	defer peerSocket.Close()
	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: actor.UIN}] = roomPeerUDPEndpoint{
		PlayerID: actor.Profile.PlayerID, Address: registeredSocket.LocalAddr().(*net.UDPAddr), Connection: serverSocket, LastUpdated: time.Now(),
	}
	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: peer.UIN}] = roomPeerUDPEndpoint{
		PlayerID: peer.Profile.PlayerID, Address: peerSocket.LocalAddr().(*net.UDPAddr), Connection: serverSocket, LastUpdated: time.Now(),
	}
	handshake := game.LegacyUDPPeerHandshake{
		Flags: game.LegacyUDPPeerHandshakeRequest, UIN: actor.UIN,
		Endpoint: game.LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: 18000},
	}
	data, err := handshake.Encode()
	if err != nil {
		t.Fatal(err)
	}
	server.handleLegacyUDPControl(serverSocket, "game-udp", "spoof", serverSocket.LocalAddr().String(), spoofSocket.LocalAddr().(*net.UDPAddr), data)
	if err = peerSocket.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = peerSocket.ReadFromUDP(make([]byte, 64)); err == nil {
		t.Fatal("unregistered peer-handshake source was relayed")
	}
}

func TestRoomPeerUDPMulticastRoutesType2BetweenMode1PresenceEndpoints(t *testing.T) {
	server, actor, peer := newRoomPeerUDPTestServer(t)
	serverSocket := listenRoomPeerUDPTest(t)
	actorMode1 := listenRoomPeerUDPTest(t)
	peerMode1 := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer actorMode1.Close()
	defer peerMode1.Close()

	actorPresence := game.LegacyUDPControlPacket{
		Header:   game.LegacyUDPControlHeader{PlayerID: actor.Profile.PlayerID, UIN: actor.UIN, Type: game.LegacyUDPPresenceType},
		Presence: &game.LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: uint16(actorMode1.LocalAddr().(*net.UDPAddr).Port)},
	}
	actorPresenceData, err := actorPresence.Encode()
	if err != nil {
		t.Fatal(err)
	}
	server.handleLegacyUDPControl(serverSocket, "game-udp", "actor-presence", serverSocket.LocalAddr().String(), actorMode1.LocalAddr().(*net.UDPAddr), actorPresenceData)
	expectLegacyUDPPresenceObserved(t, actorMode1, serverSocket, actorPresence)

	peerPresence := game.LegacyUDPControlPacket{
		Header:   game.LegacyUDPControlHeader{PlayerID: peer.Profile.PlayerID, UIN: peer.UIN, Type: game.LegacyUDPPresenceType},
		Presence: &game.LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: uint16(peerMode1.LocalAddr().(*net.UDPAddr).Port)},
	}
	peerPresenceData, err := peerPresence.Encode()
	if err != nil {
		t.Fatal(err)
	}
	server.handleLegacyUDPControl(serverSocket, "game-udp", "peer-presence", serverSocket.LocalAddr().String(), peerMode1.LocalAddr().(*net.UDPAddr), peerPresenceData)
	expectLegacyUDPPresenceObserved(t, peerMode1, serverSocket, peerPresence)

	multicast := game.LegacyUDPControlPacket{
		Header:    game.LegacyUDPControlHeader{PlayerID: actor.Profile.PlayerID, UIN: actor.UIN, Type: game.LegacyUDPMulticastType, PacketNumber: 7},
		Multicast: &game.LegacyUDPMulticastPayload{Targets: []game.LegacyUDPMulticastTarget{{PlayerID: peer.Profile.PlayerID, UIN: peer.UIN}}, Data: []byte{1, 2, 3, 4}},
	}
	multicastData, err := multicast.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "actor-multicast", serverSocket.LocalAddr().String(), actorMode1.LocalAddr().(*net.UDPAddr), multicastData) {
		t.Fatal("Type-2 multicast was not handled")
	}
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "actor-multicast-repeat", serverSocket.LocalAddr().String(), actorMode1.LocalAddr().(*net.UDPAddr), multicastData) {
		t.Fatal("second Type-2 multicast was not handled")
	}

	if err := peerMode1.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		got := make([]byte, game.LegacyUDPMaxDatagramSize)
		n, from, readErr := peerMode1.ReadFromUDP(got)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if from.Port != serverSocket.LocalAddr().(*net.UDPAddr).Port {
			t.Fatalf("Type-2 source = %s, want server port %d", from, serverSocket.LocalAddr().(*net.UDPAddr).Port)
		}
		if !bytes.Equal(got[:n], multicastData) {
			t.Fatalf("routed Type-2 copy %d\n got %x\nwant %x", index, got[:n], multicastData)
		}
	}
}

func TestRoomPeerRelayDiagnosticsAggregateWithoutControllingDelivery(t *testing.T) {
	server := &Server{roomPeerRelayStats: make(map[roomPeerRelayDiagnosticKey]roomPeerRelayDiagnostic)}
	now := time.Unix(100, 0)
	if _, _, report := server.recordRoomPeerRelayDiagnostic(7, 1_000_001, 2, 200, now); report {
		t.Fatal("first diagnostic sample reported an incomplete window")
	}
	if _, _, report := server.recordRoomPeerRelayDiagnostic(7, 1_000_001, 2, 300, now.Add(500*time.Millisecond)); report {
		t.Fatal("sub-second diagnostic sample reported early")
	}
	packets, bytes, report := server.recordRoomPeerRelayDiagnostic(7, 1_000_001, 2, 400, now.Add(time.Second))
	if !report || packets != 3 || bytes != 900 {
		t.Fatalf("diagnostic aggregate = packets %d bytes %d report %v", packets, bytes, report)
	}
}

func TestCompetitiveAISceneBatchStaysByteIdenticalType2(t *testing.T) {
	const (
		roomID uint16 = 7
		gameID uint32 = 77
	)
	server, authority, recipient := newRoomPeerUDPTestServer(t)
	authority.CurrentGameID, recipient.CurrentGameID = gameID, gameID

	// Replace the UDP-focused helper's drained peer pipe with an observable TCP
	// connection. Active production sessions always have a recent packet
	// template from which the per-recipient 0x0084 envelope is built.
	oldConnection := recipient.connection
	recipientServer, recipientClient := net.Pipe()
	t.Cleanup(func() {
		recipientServer.Close()
		recipientClient.Close()
	})
	server.liveMu.Lock()
	delete(server.liveSessions, oldConnection)
	recipient.connection = recipientServer
	recipient.connectionID = "recipient-tcp"
	recipient.localAddress = "local"
	recipient.remoteAddress = "127.0.0.1:41002"
	server.liveSessions[recipientServer] = recipient
	server.liveMu.Unlock()
	recipient.notePacket(testLocalRoutedPacket(t, game.ReadyCommand, 3, 0xffff, roomID, recipient.UIN))

	participants := []match.CompetitiveParticipant{
		{PlayerID: authority.Profile.PlayerID, RoleID: 1, TeamID: 1, Source: match.CompetitiveParticipantHuman},
		{PlayerID: recipient.Profile.PlayerID, RoleID: 2, TeamID: 1, Source: match.CompetitiveParticipantHuman},
		{PlayerID: 20001, RoleID: 3, TeamID: 2, Source: match.CompetitiveParticipantVirtualAI},
	}
	battle, err := match.NewCompetitiveBattle(gameID, 1, authority.Profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &liveCompetitiveAIRuntime{
		roomID: roomID, gameID: gameID,
		humanIDs:   map[uint16]struct{}{authority.Profile.PlayerID: {}, recipient.Profile.PlayerID: {}},
		virtualIDs: map[uint16]struct{}{20001: {}},
		stop:       make(chan struct{}),
	}
	server.competitiveAIRuntime = map[uint32]*liveCompetitiveAIRuntime{gameID: runtime}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	serverSocket := listenRoomPeerUDPTest(t)
	authorityMode1 := listenRoomPeerUDPTest(t)
	recipientMode1 := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer authorityMode1.Close()
	defer recipientMode1.Close()
	for _, setup := range []struct {
		session *connectionSession
		socket  *net.UDPConn
	}{{authority, authorityMode1}, {recipient, recipientMode1}} {
		presence := game.LegacyUDPControlPacket{
			Header: game.LegacyUDPControlHeader{PlayerID: setup.session.Profile.PlayerID, UIN: setup.session.UIN, Type: game.LegacyUDPPresenceType},
			Presence: &game.LegacyUDPEndpoint{
				IPv4: [4]byte{127, 0, 0, 1}, Port: uint16(setup.socket.LocalAddr().(*net.UDPAddr).Port),
			},
		}
		encoded, encodeErr := presence.Encode()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		server.handleLegacyUDPControl(serverSocket, "game-udp", "presence", serverSocket.LocalAddr().String(), setup.socket.LocalAddr().(*net.UDPAddr), encoded)
		expectLegacyUDPPresenceObserved(t, setup.socket, serverSocket, presence)
	}

	body, err := (game.DispatchItemData{
		Time:  93140,
		Items: []game.GameItem{{ItemID: 3, Row: 1, Col: 8}},
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	movementBody, err := (game.PlayerMove{PlayerID: authority.Profile.PlayerID, Entries: []game.PlayerMoveEntry{{
		PlayerID: authority.Profile.PlayerID,
		Move: game.PlayerMoveSequence{
			Sequence: 8, TimeStamp: 93120, CurrentPosX: 320, CurrentPosY: 41,
			EndPosX: 400, EndPosY: 41, WalkAndDirection: 0x10, Speed: 8,
		},
	}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	packages := []game.GameplayDataPackage{{
		PlayerID: authority.Profile.PlayerID, Time: 93140, GameID: gameID, MessageIndexes: []uint32{16, 17},
		Messages: []game.BattleMessageData{
			{Time: 93120, DataID: uint32(game.PlayerMoveSchema), Sequence: 16, Data: movementBody, GameTime: 93120},
			{Time: 93140, DataID: game.NotifyDispatchItem, Sequence: 17, Data: body, GameTime: 93140},
		},
	}}
	payload, err := game.EncodeQQTPPPGameplayBatch(game.GameplayBatch{
		GameID: gameID, Entries: []game.GameplayBatchEntry{{Index: 16, Package: packages[0]}},
	})
	if err != nil {
		t.Fatal(err)
	}
	multicastData, err := (game.LegacyUDPControlPacket{
		Header: game.LegacyUDPControlHeader{
			PlayerID: authority.Profile.PlayerID, UIN: authority.UIN,
			Type: game.LegacyUDPMulticastType, PacketNumber: 9,
		},
		Multicast: &game.LegacyUDPMulticastPayload{
			Targets: []game.LegacyUDPMulticastTarget{{PlayerID: recipient.Profile.PlayerID, UIN: recipient.UIN}},
			Data:    payload,
		},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		server.handleLegacyUDPControl(
			serverSocket, "game-udp", "authority-pickup", serverSocket.LocalAddr().String(),
			authorityMode1.LocalAddr().(*net.UDPAddr), multicastData,
		)
		close(done)
	}()
	_ = recipientMode1.SetReadDeadline(time.Now().Add(time.Second))
	udpData := make([]byte, game.LegacyUDPMaxDatagramSize)
	n, _, readErr := recipientMode1.ReadFromUDP(udpData)
	if readErr != nil {
		t.Fatal(readErr)
	}
	<-done
	if !bytes.Equal(udpData[:n], multicastData) {
		t.Fatalf("routed Type-2 batch was rewritten\n got %x\nwant %x", udpData[:n], multicastData)
	}
	routed, err := game.DecodeLegacyUDPControlPacket(udpData[:n])
	if err != nil {
		t.Fatal(err)
	}
	forwarded, err := game.DecodeQQTPPPGameplayBatch(routed.Multicast.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(forwarded.Entries) != 1 || forwarded.Entries[0].Index != 16 ||
		len(forwarded.Entries[0].Package.MessageIndexes) != 2 ||
		forwarded.Entries[0].Package.MessageIndexes[0] != 16 || forwarded.Entries[0].Package.MessageIndexes[1] != 17 ||
		len(forwarded.Entries[0].Package.Messages) != 2 ||
		forwarded.Entries[0].Package.Messages[0].DataID != uint32(game.PlayerMoveSchema) ||
		forwarded.Entries[0].Package.Messages[1].DataID != game.NotifyDispatchItem {
		t.Fatalf("forwarded causal batch = %+v, want original indexes and movement -> dispatch order", forwarded)
	}

	// Neither 0x0FAE nor any neighbour from its native batch may be converted
	// into an unsolicited 0x0084 scene notification.
	_ = recipientClient.SetReadDeadline(time.Now().Add(75 * time.Millisecond))
	if n, readErr := recipientClient.Read(make([]byte, 1)); readErr == nil {
		t.Fatalf("native scene batch produced an unexpected TCP projection (%d bytes)", n)
	} else if timeout, ok := readErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("wait for absent TCP projection: %v", readErr)
	}
}

func TestRoomPeerUDPMulticastSkipsDepartedTargetAndKeepsLiveTarget(t *testing.T) {
	server, actor, departed := newRoomPeerUDPTestServer(t)
	liveServer, liveClient := net.Pipe()
	defer liveServer.Close()
	defer liveClient.Close()
	liveProfile := game.DefaultPlayerProfile()
	liveProfile.PlayerID, liveProfile.GameInfo.RoleID = 3, 9
	live := &connectionSession{UIN: 1_000_003, Profile: liveProfile, remoteAddress: "127.0.0.1:41003"}
	if err := server.worldState().EnterLobby(live.UIN, live.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(live.UIN, live.Profile, 7, live.Profile.GameInfo.RoleID, 3); err != nil {
		t.Fatal(err)
	}
	live.setRoomID(7)
	live.worldUIN = live.UIN
	live.liveUIN.Store(live.UIN)
	live.liveRoomID.Store(7)
	server.liveSessions[liveServer] = live

	// Model the between-stage transition: the old client may still mention
	// this authenticated player, but it is no longer a member or a recipient.
	if _, err := server.worldState().LeaveRoom(departed.UIN, roomstate.LeaveEliminatedBetweenStages); err != nil {
		t.Fatal(err)
	}
	departed.setRoomID(0)

	actorEndpoint := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 42001}
	liveEndpoint := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 42003}
	server.roomPeerUDPPresence[actor.UIN] = roomPeerUDPEndpoint{PlayerID: actor.Profile.PlayerID, Address: actorEndpoint, Connection: listenRoomPeerUDPTest(t)}
	defer server.roomPeerUDPPresence[actor.UIN].Connection.Close()
	server.roomPeerUDPPresence[live.UIN] = roomPeerUDPEndpoint{PlayerID: live.Profile.PlayerID, Address: liveEndpoint, Connection: listenRoomPeerUDPTest(t)}
	defer server.roomPeerUDPPresence[live.UIN].Connection.Close()

	packet := game.LegacyUDPControlPacket{
		Header: game.LegacyUDPControlHeader{PlayerID: actor.Profile.PlayerID, UIN: actor.UIN, Type: game.LegacyUDPMulticastType},
		Multicast: &game.LegacyUDPMulticastPayload{
			Targets: []game.LegacyUDPMulticastTarget{
				{PlayerID: departed.Profile.PlayerID, UIN: departed.UIN},
				{PlayerID: live.Profile.PlayerID, UIN: live.UIN},
			},
			Data: []byte{1, 2, 3},
		},
	}
	_, _, recipients, _, err := server.resolveLegacyUDPMulticastTargets(packet, actorEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].PlayerID != live.Profile.PlayerID {
		t.Fatalf("resolved recipients = %+v, want only live player %d", recipients, live.Profile.PlayerID)
	}
}

func TestRoomPeerUDPPresenceReturnsServerObservedEndpoint(t *testing.T) {
	server, actor, _ := newRoomPeerUDPTestServer(t)
	serverSocket := listenRoomPeerUDPTest(t)
	actorSocket := listenRoomPeerUDPTest(t)
	defer serverSocket.Close()
	defer actorSocket.Close()

	request := game.LegacyUDPControlPacket{
		Header: game.LegacyUDPControlHeader{
			PacketNumber: 17, PlayerID: actor.Profile.PlayerID, UIN: actor.UIN,
			Type: game.LegacyUDPPresenceType,
		},
		Presence: &game.LegacyUDPEndpoint{IPv4: [4]byte{192, 168, 56, 1}, Port: 32000},
	}
	data, err := request.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !server.handleLegacyUDPControl(serverSocket, "game-udp", "presence", serverSocket.LocalAddr().String(), actorSocket.LocalAddr().(*net.UDPAddr), data) {
		t.Fatal("Type-1 presence was not handled")
	}
	expectLegacyUDPPresenceObserved(t, actorSocket, serverSocket, request)
}

func TestRoomPeerUDPPruneRetainsMode1Presence(t *testing.T) {
	server, _, _ := newRoomPeerUDPTestServer(t)
	stale := time.Now().Add(-roomPeerUDPMode0EndpointLifetime - time.Second)
	key := roomPeerUDPKey{RoomID: 7, UIN: 1_000_001}
	server.roomPeerUDPEndpoints[key] = roomPeerUDPEndpoint{LastUpdated: stale}
	server.roomPeerUDPPresence[key.UIN] = roomPeerUDPEndpoint{PlayerID: 1, LastUpdated: stale}

	server.roomPeerUDPMu.Lock()
	server.pruneRoomPeerUDPEndpointsLocked(time.Now())
	server.roomPeerUDPMu.Unlock()

	if _, found := server.roomPeerUDPEndpoints[key]; found {
		t.Fatal("stale mode-0 endpoint was not pruned")
	}
	if _, found := server.roomPeerUDPPresence[key.UIN]; !found {
		t.Fatal("session-scoped mode-1 presence was time-pruned")
	}
}

func TestClearRoomPeerUDPIdentityRemovesBothModes(t *testing.T) {
	server, _, _ := newRoomPeerUDPTestServer(t)
	uin := uint32(1_000_001)
	server.roomPeerUDPPresence[uin] = roomPeerUDPEndpoint{PlayerID: 1}
	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: uin}] = roomPeerUDPEndpoint{PlayerID: 1}
	server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: 1_000_002}] = roomPeerUDPEndpoint{PlayerID: 2}

	server.clearRoomPeerUDPIdentity(uin)

	if _, found := server.roomPeerUDPPresence[uin]; found {
		t.Fatal("mode-1 presence survived identity cleanup")
	}
	if _, found := server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: uin}]; found {
		t.Fatal("mode-0 endpoint survived identity cleanup")
	}
	if _, found := server.roomPeerUDPEndpoints[roomPeerUDPKey{RoomID: 7, UIN: 1_000_002}]; !found {
		t.Fatal("identity cleanup removed another account")
	}
}

func TestRoomPeerUDPRejectsCrossRoomTarget(t *testing.T) {
	server, actor, peer := newRoomPeerUDPTestServer(t)
	peer.setRoomID(8)
	packetBytes := encodeRoomPeerUDPTest(t, actor, peer, 40001)
	packet, err := game.DecodeLegacyUDPControlPacket(packetBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.validateRoomPeerUDPSender(packet, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 40001}); err == nil {
		t.Fatal("cross-room peer endpoint was accepted")
	}
}

func newRoomPeerUDPTestServer(t *testing.T) (*Server, *connectionSession, *connectionSession) {
	t.Helper()
	actorConnection, actorClient := net.Pipe()
	peerConnection, peerClient := net.Pipe()
	t.Cleanup(func() {
		actorConnection.Close()
		actorClient.Close()
		peerConnection.Close()
		peerClient.Close()
	})
	actorProfile := game.DefaultPlayerProfile()
	actorProfile.PlayerID, actorProfile.GameInfo.RoleID = 1, 7
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID, peerProfile.GameInfo.RoleID = 2, 8
	actor := &connectionSession{UIN: 1_000_001, Profile: actorProfile, remoteAddress: "127.0.0.1:41001"}
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, remoteAddress: "127.0.0.1:41002"}
	actor.connection = actorConnection
	peer.connection = peerConnection
	// Several room-level paths also mirror state over TCP. Keep the client
	// sides drained so a synchronous net.Pipe write cannot stall an otherwise
	// UDP-focused regression test.
	go io.Copy(io.Discard, actorClient)
	go io.Copy(io.Discard, peerClient)
	server := &Server{
		liveSessions: map[net.Conn]*connectionSession{
			actorConnection: actor,
			peerConnection:  peer,
		},
		roomPeerUDPEndpoints: make(map[roomPeerUDPKey]roomPeerUDPEndpoint),
		roomPeerUDPPresence:  make(map[uint32]roomPeerUDPEndpoint),
		roomPeerRelayStats:   make(map[roomPeerRelayDiagnosticKey]roomPeerRelayDiagnostic),
		logWriter:            io.Discard,
	}
	if err := server.createSessionRoom(actor, 7, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, 7, peer.Profile.GameInfo.RoleID, 2); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(7)
	peer.worldUIN = peer.UIN
	actor.setUIN(actor.UIN)
	peer.setUIN(peer.UIN)
	actor.liveRoomID.Store(7)
	peer.liveRoomID.Store(7)
	captureWriter, err := capture.Open(t.TempDir() + "/capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { captureWriter.Close() })
	server.capture = captureWriter
	return server, actor, peer
}

func listenRoomPeerUDPTest(t *testing.T) *net.UDPConn {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func expectLegacyUDPPresenceObserved(t *testing.T, client, serverSocket *net.UDPConn, request game.LegacyUDPControlPacket) {
	t.Helper()
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, game.LegacyUDPMaxDatagramSize)
	n, from, err := client.ReadFromUDP(data)
	if err != nil {
		t.Fatal(err)
	}
	if from.Port != serverSocket.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("Type-1 response source = %s, want server port %d", from, serverSocket.LocalAddr().(*net.UDPAddr).Port)
	}
	response, err := game.DecodeLegacyUDPControlPacket(data[:n])
	if err != nil {
		t.Fatal(err)
	}
	observed, err := observedLegacyUDPEndpoint(client.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Type != game.LegacyUDPPresenceType || response.Header.PacketNumber != request.Header.PacketNumber || response.Header.PlayerID != request.Header.PlayerID || response.Header.UIN != request.Header.UIN {
		t.Fatalf("Type-1 response header = %+v, want request identity and packet number %+v", response.Header, request.Header)
	}
	if response.Presence == nil || *response.Presence != observed {
		t.Fatalf("Type-1 response endpoint = %+v, want server-observed %+v", response.Presence, observed)
	}
	_ = client.SetReadDeadline(time.Time{})
}

func encodeRoomPeerUDPTest(t *testing.T, sender, target *connectionSession, port uint16) []byte {
	t.Helper()
	packet := game.LegacyUDPControlPacket{
		Header: game.LegacyUDPControlHeader{
			PlayerID: sender.Profile.PlayerID, UIN: sender.UIN,
			Type: game.LegacyUDPRoomPeerType, PacketNumber: 1,
		},
		RoomPeer: &game.LegacyUDPRoomPeerPayload{
			TargetPlayerID: target.Profile.PlayerID, TargetUIN: target.UIN,
			Endpoint: game.LegacyUDPEndpoint{IPv4: [4]byte{127, 0, 0, 1}, Port: port},
		},
	}
	data, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}
