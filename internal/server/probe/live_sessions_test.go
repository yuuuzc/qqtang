package probe

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/game"
)

func TestLegacyHeartbeatKeepsDetachedShopSessionAuthorized(t *testing.T) {
	server := &Server{
		liveSessions:        make(map[net.Conn]*connectionSession),
		authenticationState: newAuthenticationState(),
		logWriter:           io.Discard,
	}
	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	server.rememberAuthenticatedSession(primary, "192.0.2.10:40000")

	heartbeat, err := hex.DecodeString("001800000000030001000f424100019c0e06c0a80294d8c6")
	if err != nil {
		t.Fatal(err)
	}
	heartbeatUIN, ok := decodeLegacyOnlineHeartbeatUIN(heartbeat)
	if !ok || heartbeatUIN != uin {
		t.Fatalf("heartbeat identity = %d, %v", heartbeatUIN, ok)
	}
	server.touchAuthenticatedSession("192.0.2.10:50000", heartbeatUIN)

	shop := &connectionSession{remoteAddress: "192.0.2.10:41000"}
	if err := server.bindAuxiliaryShopConnection(shop, uin); err != nil {
		t.Fatal(err)
	}
	if !shop.auxiliary || shop.UIN != uin || shop.Profile.Nickname != "糖一" {
		t.Fatalf("resumed shop session = %+v", shop)
	}
}

func TestAccountOnlineForGMIncludesLiveConnectionAndReconnectLease(t *testing.T) {
	const uin uint32 = 1_000_001
	session := &connectionSession{UIN: uin, Profile: game.PlayerProfile{PlayerID: 1}}
	session.liveUIN.Store(uin)
	server := &Server{
		liveSessions:        map[net.Conn]*connectionSession{nil: session},
		authenticationState: newAuthenticationState(),
	}
	if !server.accountOnlineForGM(uin) {
		t.Fatal("live owning connection was not reported online")
	}

	server.rememberAuthenticatedSession(session, "192.0.2.10:40000")
	session.liveUIN.Store(0)
	if !server.accountOnlineForGM(uin) {
		t.Fatal("authenticated reconnect lease was not reported online")
	}
	if server.accountOnlineForGM(uin + 1) {
		t.Fatal("unrelated account was reported online")
	}
}

func TestShopConnectionsShareEndpointWithoutReplacingHall(t *testing.T) {
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	primaryServer, primaryClient := net.Pipe()
	catalogServer, catalogClient := net.Pipe()
	sessionServer, sessionClient := net.Pipe()
	defer primaryServer.Close()
	defer primaryClient.Close()
	defer catalogServer.Close()
	defer catalogClient.Close()
	defer sessionServer.Close()
	defer sessionClient.Close()

	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	catalog := &connectionSession{}
	shopSession := &connectionSession{}
	server.registerLiveSession(primary, primaryServer, "hall", "127.0.0.1:18000", "192.0.2.10:40000")
	server.registerLiveSession(catalog, catalogServer, "shop-type3", "127.0.0.1:18001", "192.0.2.10:40001")
	server.registerLiveSession(shopSession, sessionServer, "shop-type2", "127.0.0.1:18001", "192.0.2.10:40002")
	if err := server.bindAuxiliaryShopConnection(catalog, uin); err != nil {
		t.Fatal(err)
	}
	packet, err := hex.DecodeString("0000008a0000000002ffffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f9b2bbf783f6fba79c99695e83b292ceba21ff302086b93c6f0ed0c2dbb68e93de0c66d9a636dcca575750c281b52b80de4147c6722bf86f29a6857e47fb9e96fca8838023b1f2c97c6e0aacb1160d067f571bb8dc993b6ee")
	if err != nil {
		t.Fatal(err)
	}
	server.prepareSessionContinuation(ListenerConfig{Response: ResponseConfig{QQTLoginSuccess: true, QQTShopType2Session: true}}, shopSession, "shop-type2", packet)
	for name, auxiliary := range map[string]*connectionSession{"catalog": catalog, "session": shopSession} {
		if !auxiliary.auxiliary || auxiliary.UIN != uin || auxiliary.liveUIN.Load() != 0 {
			t.Fatalf("%s connection = auxiliary:%v UIN:%d live:%d", name, auxiliary.auxiliary, auxiliary.UIN, auxiliary.liveUIN.Load())
		}
	}
	if primary.liveUIN.Load() != uin || primary.continuation {
		t.Fatalf("hall ownership changed = live:%d continuation:%v", primary.liveUIN.Load(), primary.continuation)
	}
	server.handleConnectionDeparture(catalog, "shop-type3")
	server.handleConnectionDeparture(shopSession, "shop-type2")
	if primary.liveUIN.Load() != uin || primary.Profile.Nickname != "糖一" {
		t.Fatalf("closing both auxiliary shop connections changed hall session: live=%d profile=%+v", primary.liveUIN.Load(), primary.Profile)
	}
}

func TestShopConnectionCannotBindAnotherRemoteHostsOnlineUIN(t *testing.T) {
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	primaryServer, primaryClient := net.Pipe()
	shopServer, shopClient := net.Pipe()
	defer primaryServer.Close()
	defer primaryClient.Close()
	defer shopServer.Close()
	defer shopClient.Close()

	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	shop := &connectionSession{}
	server.registerLiveSession(primary, primaryServer, "hall", "127.0.0.1:18000", "192.0.2.10:40000")
	server.registerLiveSession(shop, shopServer, "shop", "127.0.0.1:18001", "192.0.2.11:40001")
	if err := server.bindAuxiliaryShopConnection(shop, uin); err == nil {
		t.Fatal("different remote host bound another player's shop session")
	}
	if shop.auxiliary || shop.UIN != 0 {
		t.Fatalf("rejected shop identity = auxiliary:%v UIN:%d", shop.auxiliary, shop.UIN)
	}
}

func TestShopConnectionsCanRebindAfterPreviousVisitCloses(t *testing.T) {
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	primaryServer, primaryClient := net.Pipe()
	defer primaryServer.Close()
	defer primaryClient.Close()

	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	server.registerLiveSession(primary, primaryServer, "hall", "127.0.0.1:18000", "192.0.2.10:40000")

	for visit := 1; visit <= 2; visit++ {
		catalogServer, catalogClient := net.Pipe()
		sessionServer, sessionClient := net.Pipe()
		catalog := &connectionSession{}
		shopSession := &connectionSession{}
		server.registerLiveSession(catalog, catalogServer, fmt.Sprintf("catalog-%d", visit), "127.0.0.1:18001", fmt.Sprintf("192.0.2.10:%d", 41000+visit*2))
		server.registerLiveSession(shopSession, sessionServer, fmt.Sprintf("session-%d", visit), "127.0.0.1:18001", fmt.Sprintf("192.0.2.10:%d", 41001+visit*2))

		if err := server.bindAuxiliaryShopConnection(catalog, uin); err != nil {
			t.Fatalf("visit %d catalog bind: %v", visit, err)
		}
		if err := server.bindAuxiliaryShopConnection(shopSession, uin); err != nil {
			t.Fatalf("visit %d session bind: %v", visit, err)
		}
		server.handleConnectionDeparture(catalog, fmt.Sprintf("catalog-%d", visit))
		server.handleConnectionDeparture(shopSession, fmt.Sprintf("session-%d", visit))
		server.unregisterLiveSession(catalogServer)
		server.unregisterLiveSession(sessionServer)
		catalogServer.Close()
		catalogClient.Close()
		sessionServer.Close()
		sessionClient.Close()

		if primary.liveUIN.Load() != uin || primary.Profile.Nickname != "糖一" {
			t.Fatalf("visit %d changed primary hall session: live=%d profile=%+v", visit, primary.liveUIN.Load(), primary.Profile)
		}
	}
}

func TestShopType2ConfigResponseClosesDisposableConnection(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()

	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		capture: captureWriter, logWriter: io.Discard,
		liveSessions: make(map[net.Conn]*connectionSession),
	}
	session := &connectionSession{}
	request, err := hex.DecodeString("0000008a0000000002ffffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f9b2bbf783f6fba79c99695e83b292ceba21ff302086b93c6f0ed0c2dbb68e93de0c66d9a636dcca575750c281b52b80de4147c6722bf86f29a6857e47fb9e96fca8838023b1f2c97c6e0aacb1160d067f571bb8dc993b6ee")
	if err != nil {
		t.Fatal(err)
	}

	kept := make(chan bool, 1)
	go func() {
		kept <- server.handleTCPMessage(serverSide, ListenerConfig{Response: ResponseConfig{
			QQTLoginSuccess: true, QQTShopType2Session: true,
		}}, session, "shop-config", "127.0.0.1:18001", "127.0.0.1:50000", request)
	}()

	header := make([]byte, 4)
	if _, err := io.ReadFull(clientSide, header); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, binary.BigEndian.Uint32(header))
	copy(response, header)
	if _, err := io.ReadFull(clientSide, response[4:]); err != nil {
		t.Fatal(err)
	}
	inspection, err := game.InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.GetConfigFileCommand {
		t.Fatalf("response command = 0x%04X", inspection.Command)
	}
	if keep := <-kept; keep {
		t.Fatal("completed type-2 config connection must be closed")
	}
}

func TestAuthenticatedLeaseAllowsSameHostLoginReconnectOnlyOnce(t *testing.T) {
	server := &Server{
		liveSessions:        make(map[net.Conn]*connectionSession),
		authenticationState: newAuthenticationState(),
		logWriter:           io.Discard,
	}
	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	server.rememberAuthenticatedSession(primary, "192.0.2.10:40000")

	firstServer, firstClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	first := &connectionSession{}
	server.registerLiveSession(first, firstServer, "first", "127.0.0.1:18000", "192.0.2.10:41000")
	if !server.claimLiveUINFromLease(first, "192.0.2.10:41000", uin) {
		t.Fatal("same-host reconnect should resume the authenticated lease")
	}
	if first.liveUIN.Load() != uin || first.Profile.Nickname != "糖一" {
		t.Fatalf("resumed session = UIN:%d profile:%+v", first.liveUIN.Load(), first.Profile)
	}

	secondServer, secondClient := net.Pipe()
	defer secondServer.Close()
	defer secondClient.Close()
	second := &connectionSession{}
	server.registerLiveSession(second, secondServer, "second", "127.0.0.1:18000", "192.0.2.10:42000")
	if server.claimLiveUINFromLease(second, "192.0.2.10:42000", uin) {
		t.Fatal("lease reconnect must not create a second live owner")
	}
	if server.claimLiveUINFromLease(&connectionSession{}, "192.0.2.11:43000", uin) {
		t.Fatal("different remote host must not resume the lease")
	}
}

func TestSessionContinuationDoesNotClaimFreshUnboundConnection(t *testing.T) {
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	primaryServer, primaryClient := net.Pipe()
	replacementServer, replacementClient := net.Pipe()
	defer primaryServer.Close()
	defer primaryClient.Close()
	defer replacementServer.Close()
	defer replacementClient.Close()

	const uin = uint32(1_000_001)
	primary := &connectionSession{UIN: uin, Profile: game.PlayerProfile{Nickname: "糖一"}}
	primary.liveUIN.Store(uin)
	replacement := &connectionSession{}
	server.registerLiveSession(primary, primaryServer, "primary", "127.0.0.1:18000", "192.0.2.10:40000")
	server.registerLiveSession(replacement, replacementServer, "replacement", "127.0.0.1:18000", "192.0.2.10:40001")
	replacement.openedAt = time.Now()

	if server.promoteSessionContinuation(primary, "primary") {
		t.Fatal("fresh unbound connection must not inherit another local client's identity")
	}
	if replacement.liveUIN.Load() != 0 || replacement.UIN != 0 || replacement.continuation {
		t.Fatalf("unbound connection was mutated = UIN:%d liveUIN:%d continuation:%v", replacement.UIN, replacement.liveUIN.Load(), replacement.continuation)
	}
}

func TestClaimLiveUINRejectsDuplicateAndIdentitySwitch(t *testing.T) {
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()

	server := &Server{liveSessions: make(map[net.Conn]*connectionSession)}
	first := &connectionSession{}
	second := &connectionSession{}
	server.registerLiveSession(first, firstServer, "first", "local", "remote")
	server.registerLiveSession(second, secondServer, "second", "local", "remote")

	if !server.claimLiveUIN(first, 1_000_001) {
		t.Fatal("first session should claim the UIN")
	}
	if server.claimLiveUIN(second, 1_000_001) {
		t.Fatal("duplicate live UIN should be rejected")
	}
	if server.claimLiveUIN(first, 1_000_002) {
		t.Fatal("one live connection must not switch UINs")
	}
	first.setUIN(0)
	if !server.claimLiveUIN(second, 1_000_001) {
		t.Fatal("released UIN should become claimable")
	}
}

func TestProjectAndBroadcastRoomMatchUsesPeerEnvelope(t *testing.T) {
	captureWriter, err := capture.Open(t.TempDir() + "/capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard, capture: captureWriter}
	peerServer, peerClient := net.Pipe()
	defer peerServer.Close()
	defer peerClient.Close()

	const (
		roomID  uint16 = 7
		gameID  uint32 = 91
		mapID   uint32 = 1601
		peerUIN uint32 = 1_000_002
	)
	peer := &connectionSession{UIN: peerUIN, Profile: game.PlayerProfile{PlayerID: 2}}
	peer.liveUIN.Store(peerUIN)
	peer.setRoomID(roomID)
	recipientTemplate := testLocalRoutedPacket(t, game.ReadyCommand, 3, 0xffff, 1, peerUIN)
	peer.notePacket(recipientTemplate)
	server.registerLiveSession(peer, peerServer, "peer", "local", "remote")

	server.projectRoomMatch(roomID, gameID, mapID)
	if peer.CurrentGameID != gameID || peer.CurrentMapID != mapID {
		t.Fatalf("peer projection = game:%d map:%d", peer.CurrentGameID, peer.CurrentMapID)
	}

	ownerStart := testLocalRoutedPacket(t, game.StartGameCommand, 3, 0xffff, 1, 1_000_001)
	notification, err := game.BuildLocalAdventureGameBegin(ownerStart, game.GameBeginData{
		GameID: gameID, MapID: mapID, SpawnSeed: 1, ItemSeed: 2, ArbitratorPlayerID: 1,
		Players:    []game.PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1}, {PlayerID: 2, RoleID: 2, TeamID: 2}},
		ContinueID: game.NoRemoteContinueFileID, GameTimeMS: game.DefaultAdventureGameTimeMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		server.broadcastMatchNotification(roomID, 1_000_001, "test_game_begin_peer", notification)
		close(done)
	}()
	_ = peerClient.SetReadDeadline(time.Now().Add(time.Second))
	header := make([]byte, 4)
	if _, err = io.ReadFull(peerClient, header); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, int(binary.BigEndian.Uint32(header)))
	copy(packet, header)
	if _, err = io.ReadFull(peerClient, packet[4:]); err != nil {
		t.Fatal(err)
	}
	<-done
	inspection, err := game.InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.GameBeginNotifyCommand || inspection.EnvelopeUIN != peerUIN {
		t.Fatalf("peer GAME_BEGIN = command:0x%04X UIN:%d", inspection.Command, inspection.EnvelopeUIN)
	}
}

func TestProjectRoomMatchFromLockedSessionDoesNotRelockActor(t *testing.T) {
	const (
		roomID uint16 = 7
		gameID uint32 = 91
		mapID  uint32 = 1601
		uin    uint32 = 1_000_001
	)
	actor := &connectionSession{Profile: game.PlayerProfile{PlayerID: 1}}
	actor.liveUIN.Store(uin)
	actor.setRoomID(roomID)
	server := &Server{
		liveSessions: map[net.Conn]*connectionSession{nil: actor},
		logWriter:    io.Discard,
	}

	done := make(chan struct{})
	go func() {
		actor.mu.Lock()
		defer actor.mu.Unlock()
		server.projectRoomMatchFromLockedSession(actor, roomID, gameID, mapID, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("locked dispatcher session was locked a second time")
	}

	actor.mu.Lock()
	defer actor.mu.Unlock()
	if actor.CurrentGameID != gameID || actor.CurrentMapID != mapID {
		t.Fatalf("actor projection = game:%d map:%d", actor.CurrentGameID, actor.CurrentMapID)
	}
}

func TestSimultaneousRoomMatchProjectionUsesPeerMailboxes(t *testing.T) {
	const roomID uint16 = 7
	one := &connectionSession{UIN: 1_000_001, Profile: game.PlayerProfile{PlayerID: 1}}
	two := &connectionSession{UIN: 1_000_002, Profile: game.PlayerProfile{PlayerID: 2}}
	for _, session := range []*connectionSession{one, two} {
		session.setUIN(session.UIN)
		session.setRoomID(roomID)
	}
	oneServer, oneClient := net.Pipe()
	twoServer, twoClient := net.Pipe()
	defer oneServer.Close()
	defer oneClient.Close()
	defer twoServer.Close()
	defer twoClient.Close()
	server := &Server{liveSessions: map[net.Conn]*connectionSession{
		oneServer: one, twoServer: two,
	}, logWriter: io.Discard}

	start := make(chan struct{})
	done := make(chan struct{}, 2)
	project := func(actor *connectionSession, gameID uint32) {
		actor.mu.Lock()
		<-start
		server.projectRoomMatchFromLockedSession(actor, roomID, gameID, 1601, nil)
		actor.mu.Unlock()
		done <- struct{}{}
	}
	go project(one, 91)
	go project(two, 92)
	close(start)
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("simultaneous room projections blocked on the peer session mutex")
		}
	}

	// Each connection actor consumes any peer command at its own serialization
	// boundary. The exact last-writer result is intentionally not asserted;
	// the invariant is a valid complete projection rather than torn fields.
	for _, session := range []*connectionSession{one, two} {
		session.mu.Lock()
		session.applyPendingProjections()
		gameID, stageGameID, mapID := session.CurrentGameID, session.CurrentStageGameID, session.CurrentMapID
		session.mu.Unlock()
		if (gameID != 91 && gameID != 92) || stageGameID != gameID || mapID != 1601 {
			t.Fatalf("mailbox projection = game:%d stage:%d map:%d", gameID, stageGameID, mapID)
		}
	}
}

func TestProjectRoomAdventureStageChangesOnlyWireIdentity(t *testing.T) {
	const (
		roomID      uint16 = 7
		matchGameID uint32 = 91
		stageGameID uint32 = 92
		mapID       uint32 = 1602
		uin         uint32 = 1_000_001
	)
	actor := &connectionSession{
		Profile: game.PlayerProfile{PlayerID: 1}, CurrentGameID: matchGameID,
		CurrentStageGameID: matchGameID, CurrentMapID: 1601,
	}
	actor.liveUIN.Store(uin)
	actor.setRoomID(roomID)
	server := &Server{liveSessions: map[net.Conn]*connectionSession{nil: actor}, logWriter: io.Discard}
	players := map[uint16]struct{}{1: {}}

	actor.mu.Lock()
	server.projectRoomAdventureStageFromLockedSession(actor, roomID, matchGameID, stageGameID, mapID, players)
	actor.mu.Unlock()

	if actor.CurrentGameID != matchGameID || actor.CurrentStageGameID != stageGameID || actor.CurrentMapID != mapID {
		t.Fatalf("stage projection = match:%d stage:%d map:%d", actor.CurrentGameID, actor.CurrentStageGameID, actor.CurrentMapID)
	}
}

func TestShopTransitionPromotesReplacementLobbyConnection(t *testing.T) {
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()

	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Nickname = "糖一"
	primary := &connectionSession{UIN: uin, Profile: profile}
	primary.liveUIN.Store(uin)
	replacement := &connectionSession{}
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	server.registerLiveSession(primary, firstServer, "primary", "127.0.0.1:18000", "192.0.2.10:40000")
	server.registerLiveSession(replacement, secondServer, "replacement", "127.0.0.1:18000", "192.0.2.10:40001")

	if err := server.bindSessionContinuation(replacement, uin); err != nil {
		t.Fatal(err)
	}
	if !replacement.continuation || replacement.UIN != uin || replacement.liveUIN.Load() != 0 {
		t.Fatalf("replacement before promotion = continuation:%v UIN:%d liveUIN:%d", replacement.continuation, replacement.UIN, replacement.liveUIN.Load())
	}
	if !server.promoteSessionContinuation(primary, "primary") {
		t.Fatal("expected replacement connection to be promoted")
	}
	if replacement.continuation || replacement.liveUIN.Load() != uin || replacement.Profile.Nickname != "糖一" {
		t.Fatalf("replacement after promotion = continuation:%v liveUIN:%d profile:%+v", replacement.continuation, replacement.liveUIN.Load(), replacement.Profile)
	}
	if primary.liveUIN.Load() != 0 {
		t.Fatalf("departing session still owns UIN %d", primary.liveUIN.Load())
	}
}

func TestSessionContinuationRejectsDifferentRemoteHost(t *testing.T) {
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()

	const uin uint32 = 1_000_001
	primary := &connectionSession{UIN: uin, Profile: game.DefaultPlayerProfile()}
	primary.liveUIN.Store(uin)
	replacement := &connectionSession{}
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession)}
	server.registerLiveSession(primary, firstServer, "primary", "127.0.0.1:18000", "192.0.2.10:40000")
	server.registerLiveSession(replacement, secondServer, "replacement", "127.0.0.1:18000", "192.0.2.11:40001")
	if err := server.bindSessionContinuation(replacement, uin); err == nil {
		t.Fatal("different remote host must not inherit an authenticated session")
	}
}
