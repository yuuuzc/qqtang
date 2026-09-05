package probe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qqtang/internal/game/mapdata"
	"qqtang/internal/game/match"
	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/game"
	"qqtang/internal/protocol/qqtea"
)

func TestTCPProbeCapturesAndResponds(t *testing.T) {
	config := Config{
		CaptureRoot: t.TempDir(), MaxPacketSize: 64, IdleTimeoutMS: 2000,
		Listeners: []ListenerConfig{{Name: "test", Network: "tcp", Address: "127.0.0.1:0", response: []byte{0xaa, 0x55}}},
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	address := server.Addresses()[0].Address
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 2)
	if _, err := connection.Read(response); err != nil {
		t.Fatal(err)
	}
	if response[0] != 0xaa || response[1] != 0x55 {
		t.Fatalf("unexpected response %x", response)
	}
	_ = connection.Close()
	cancel()
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(server.SessionPath(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var records []capture.Record
	for scanner.Scan() {
		var record capture.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 capture records, got %d", len(records))
	}
	if records[0].Direction != "client_to_server" || records[0].Hex != "010203" {
		t.Fatalf("unexpected receive record %+v", records[0])
	}
	if records[1].Direction != "server_to_client" || records[1].Hex != "aa55" {
		t.Fatalf("unexpected send record %+v", records[1])
	}
}

func TestAdventurePVEBossDataSelectsConfiguredCountDeterministically(t *testing.T) {
	stage := mapdata.AdventureStageRule{
		MapID: 1601,
		NPCDropGroups: []mapdata.AdventureNPCDropGroup{{
			BossID: 30033, BossCount: 4, DropCount: 1, DropChance: 100,
			NormalItems: []mapdata.AdventureNPCDropItem{
				{ItemID: 30067, Quantity: 1, Weight: 1},
				{ItemID: 30068, Quantity: 1, Weight: 1},
				{ItemID: 30069, Quantity: 1, Weight: 1},
			},
		}},
	}
	want := adventurePVEBossData(stage, 0x12345678)
	got := adventurePVEBossData(stage, 0x12345678)
	if len(got.Bosses) != 1 || len(got.Bosses[0].NormalItems) != 1 {
		t.Fatalf("selected boss data = %+v", got)
	}
	if got.Bosses[0].NormalItems[0] != want.Bosses[0].NormalItems[0] {
		t.Fatalf("same seed selected %v, want %v", got.Bosses[0].NormalItems[0], want.Bosses[0].NormalItems[0])
	}
}

func TestTCPProbeEchoesReceivedBytes(t *testing.T) {
	config := Config{
		CaptureRoot: t.TempDir(), MaxPacketSize: 64, IdleTimeoutMS: 2000,
		Listeners: []ListenerConfig{{
			Name: "echo", Network: "tcp", Address: "127.0.0.1:0",
			Response: ResponseConfig{EchoAfterReceive: true},
		}},
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", server.Addresses()[0].Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x03, 0x7f}
	if _, err := connection.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := connection.Read(got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("unexpected echo %x", got)
	}
	_ = connection.Close()
	cancel()
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestTCPProbeLimitsResponsesAcrossConnections(t *testing.T) {
	config := Config{
		CaptureRoot: t.TempDir(), MaxPacketSize: 64, IdleTimeoutMS: 2000,
		Listeners: []ListenerConfig{{
			Name: "limited", Network: "tcp", Address: "127.0.0.1:0",
			Response: ResponseConfig{MaxSends: 1}, response: []byte{0xAA},
		}},
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	address := server.Addresses()[0].Address
	for index := 0; index < 2; index++ {
		connection, dialErr := net.DialTimeout("tcp", address, time.Second)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		if _, writeErr := connection.Write([]byte{byte(index)}); writeErr != nil {
			t.Fatal(writeErr)
		}
		_ = connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buffer := make([]byte, 1)
		_, readErr := connection.Read(buffer)
		if index == 0 && (readErr != nil || buffer[0] != 0xAA) {
			t.Fatalf("first response = %X, %v", buffer, readErr)
		}
		if index == 1 && readErr == nil {
			t.Fatal("second connection unexpectedly received a response")
		}
		_ = connection.Close()
	}
	cancel()
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestQQTDirectoryResponseIsRequestGatedAcrossConnections(t *testing.T) {
	config := Config{
		CaptureRoot: t.TempDir(), MaxPacketSize: 1024, IdleTimeoutMS: 2000,
		DirectoryHall: &DirectoryHallConfig{ServerIP: "127.0.0.1", ServerPort: 18000, ServerUDPPort: 18000},
		Listeners: []ListenerConfig{{
			Name: "directory", Network: "tcp", Address: "127.0.0.1:0",
			Response: ResponseConfig{QQTDirectoryResponse: true},
		}},
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	address := server.Addresses()[0].Address
	for index := 0; index < 2; index++ {
		connection, dialErr := net.DialTimeout("tcp", address, time.Second)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		if _, writeErr := connection.Write(testLocalRoutedPacket(t, localDirectoryRequestCommand, localDirectoryRequestRoute, localDirectoryRequestMarker, localDirectoryRequestSectionID, uint32(1_000_001+index))); writeErr != nil {
			t.Fatal(writeErr)
		}
		responseHeader := make([]byte, 4)
		if _, readErr := io.ReadFull(connection, responseHeader); readErr != nil {
			t.Fatalf("connection %d directory response header: %v", index, readErr)
		}
		response := make([]byte, int(binary.BigEndian.Uint32(responseHeader)))
		copy(response, responseHeader)
		if _, readErr := io.ReadFull(connection, response[4:]); readErr != nil {
			t.Fatalf("connection %d directory response body: %v", index, readErr)
		}
		inspection, inspectErr := game.InspectLocalPacket(response)
		if inspectErr != nil || inspection.EnvelopeUIN != uint32(1_000_001+index) || inspection.OuterSequence != 0 {
			t.Fatalf("connection %d rebound directory response = %+v, %v", index, inspection, inspectErr)
		}
		if inspection.InnerSequence != 1 || inspection.Route != localDirectoryRequestRoute ||
			inspection.Marker != localDirectoryRequestMarker || inspection.SectionID != 0x00FE {
			t.Fatalf("connection %d directory response route is unexpected: %+v", index, inspection)
		}
		if _, writeErr := connection.Write(testLocalRoutedPacket(t, 0x0107, 4, 0xFFFF, 0, uint32(1_000_001+index))); writeErr != nil {
			t.Fatal(writeErr)
		}
		_ = connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, readErr := connection.Read(responseHeader); readErr == nil {
			t.Fatalf("connection %d acknowledgement unexpectedly received a directory response", index)
		}
		_ = connection.Close()
	}
	cancel()
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
}

func testLocalRoutedPacket(t *testing.T, command, route, marker, sectionID uint16, uin uint32) []byte {
	return testLocalRoutedPacketWithPayload(t, command, route, marker, sectionID, uin, nil)
}

func testLocalRoutedPacketWithPayload(t *testing.T, command, route, marker, sectionID uint16, uin uint32, payload []byte) []byte {
	t.Helper()
	plaintext := make([]byte, 14+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:2], command)
	binary.BigEndian.PutUint16(plaintext[6:8], 1)
	binary.BigEndian.PutUint16(plaintext[8:10], route)
	binary.BigEndian.PutUint16(plaintext[10:12], marker)
	binary.BigEndian.PutUint16(plaintext[12:14], sectionID)
	copy(plaintext[14:], payload)
	ciphertext, err := qqtea.EncryptWithReader(plaintext, directory.LocalKey, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 50+len(ciphertext))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(packet)))
	binary.BigEndian.PutUint32(packet[12:16], uin)
	packet[16], packet[17] = 1, byte(len(directory.LocalST))
	copy(packet[18:50], directory.LocalST)
	copy(packet[50:], ciphertext)
	return packet
}

func TestTCPProbeFramesCoalescedQQTPackets(t *testing.T) {
	config := Config{
		CaptureRoot: t.TempDir(), MaxPacketSize: 64, IdleTimeoutMS: 2000,
		Listeners: []ListenerConfig{{
			Name: "framed", Network: "tcp", Address: "127.0.0.1:0",
			Response: ResponseConfig{QQTLoginSuccess: true},
		}},
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", server.Addresses()[0].Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{0, 0, 0, 4, 0, 0, 0, 4}); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(server.SessionPath(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var records []capture.Record
	for scanner.Scan() {
		var record capture.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want two framed capture records, got %d", len(records))
	}
	for index, record := range records {
		if record.Hex != "00000004" {
			t.Fatalf("record %d = %s", index, record.Hex)
		}
	}
}

func TestAccountAuthGrantIsBoundToHostAndConsumedOnce(t *testing.T) {
	server := &Server{authenticationState: newAuthenticationState(), logWriter: io.Discard}
	server.grantAccountAuth("192.0.2.10:41000", 1_000_001)
	if server.consumeAccountAuth("192.0.2.11:42000", 1_000_001) {
		t.Fatal("password grant was accepted from another host")
	}
	if !server.consumeAccountAuth("192.0.2.10:43000", 1_000_001) {
		t.Fatal("password grant was not accepted for the same host and account")
	}
	if server.consumeAccountAuth("192.0.2.10:44000", 1_000_001) {
		t.Fatal("password grant was reusable")
	}
}

func TestDirectoryNavigationSurvivesSelectorConnectionHandoff(t *testing.T) {
	server := &Server{authenticationState: newAuthenticationState(), logWriter: io.Discard}
	remote := "192.0.2.10:41000"
	const uin = uint32(1_000_001)
	server.grantAccountAuth(remote, uin)
	session := &connectionSession{remoteAddress: remote, connectionID: "directory-test"}
	if !server.attachAccountAuthNavigation(session, remote, uin) {
		t.Fatal("validated directory connection did not attach to the account grant")
	}
	server.authMu.Lock()
	server.pendingAuth[accountAuthGrant{RemoteHost: remoteHost(remote), UIN: uin}] = time.Now().Add(-time.Second)
	server.authMu.Unlock()
	server.handleConnectionDeparture(session, "directory-test")
	if server.consumeAccountAuth("192.0.2.11:42000", uin) {
		t.Fatal("directory handoff grant was accepted from another host")
	}
	if !server.consumeAccountAuth("192.0.2.10:43000", uin) {
		t.Fatal("directory handoff did not renew the one-time grant for the same host")
	}
	if server.consumeAccountAuth("192.0.2.10:44000", uin) {
		t.Fatal("directory handoff grant was reusable")
	}
}

func TestDirectoryNavigationConsumesPasswordGrantIntoSeparateCapability(t *testing.T) {
	server := &Server{authenticationState: newAuthenticationState(), logWriter: io.Discard}
	remote := "192.0.2.10:41000"
	const uin = uint32(1_000_001)
	server.grantAccountAuth(remote, uin)
	key := accountAuthGrant{RemoteHost: remoteHost(remote), UIN: uin}
	server.authMu.Lock()
	originalExpiry := server.pendingAuth[key]
	server.authMu.Unlock()

	session := &connectionSession{remoteAddress: remote, connectionID: "directory-test"}
	if !server.attachAccountAuthNavigation(session, remote, uin) {
		t.Fatal("validated directory connection did not attach to the account grant")
	}
	if !server.handoffAccountAuthNavigation(session) {
		t.Fatal("directory navigation did not hand off its account grant")
	}

	server.authMu.Lock()
	_, passwordGrantRemains := server.pendingAuth[key]
	handoffExpiry := server.navigationAuth[key]
	server.authMu.Unlock()
	if passwordGrantRemains {
		t.Fatal("directory navigation left the password grant reusable")
	}
	if !handoffExpiry.After(originalExpiry) {
		t.Fatalf("directory navigation expiry %s did not outlive password grant %s", handoffExpiry, originalExpiry)
	}
}

func TestLiveDirectoryNavigationIsOneTimeAuthAfterGrantExpires(t *testing.T) {
	server := &Server{
		authenticationState: newAuthenticationState(),
		liveSessions:        make(map[net.Conn]*connectionSession),
	}
	remote := "192.0.2.10:41000"
	const uin = uint32(1_000_001)
	server.grantAccountAuth(remote, uin)
	navigation := &connectionSession{remoteAddress: remote}
	if !server.attachAccountAuthNavigation(navigation, remote, uin) {
		t.Fatal("validated directory connection did not attach to the account grant")
	}
	server.liveSessions[nil] = navigation
	server.authMu.Lock()
	server.pendingAuth[accountAuthGrant{RemoteHost: remoteHost(remote), UIN: uin}] = time.Now().Add(-time.Second)
	server.authMu.Unlock()
	if server.consumeAccountAuth("192.0.2.11:42000", uin) {
		t.Fatal("live directory capability was accepted from another host")
	}
	if !server.consumeAccountAuth("192.0.2.10:43000", uin) {
		t.Fatal("live directory capability did not bridge the expired grant")
	}
	if server.consumeAccountAuth("192.0.2.10:44000", uin) {
		t.Fatal("live directory capability was reusable")
	}
}

func TestNewAdventureGameDataUsesInstalledContinuationResources(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.ExtPoint = 5_200_000
	selectedMap := mapdata.AdventureMap{ID: 1607, SequenceID: 2, MapIndex: 0}
	session := &connectionSession{UIN: 1_000_001, Profile: profile, CurrentGameID: 7}
	copy(selectedMap.MapHash[:], "0123456789ABCDEF0123456789ABCDEF")

	server := &Server{}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	data, err := server.newAdventureGameData(session, session.CurrentGameID, selectedMap, profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	if data.MapID != selectedMap.ID {
		t.Fatalf("map ID = %d, want %d", data.MapID, selectedMap.ID)
	}
	if data.ContinueID != game.NoRemoteContinueFileID {
		t.Fatalf("continue ID = %d, want installed-resource sentinel %d", data.ContinueID, game.NoRemoteContinueFileID)
	}
	if data.FileHash != [game.GameBeginFileHashSize]byte{} {
		t.Fatalf("file hash = %X, want all zeroes for installed resources", data.FileHash)
	}
	if data.Players[0].ExtPoint != profile.GameInfo.ExtPoint {
		t.Fatalf("adventure points = %d, want %d", data.Players[0].ExtPoint, profile.GameInfo.ExtPoint)
	}
	if len(data.Items) != 0 {
		t.Fatalf("ordinary timed-delivery item pool = %+v, want empty in adventure", data.Items)
	}
	if len(data.NewItems) != 0 {
		t.Fatalf("adventure initial scene elements = %+v, want empty by default", data.NewItems)
	}
}

func TestNewAdventureGameDataUsesPerStageWireIdentity(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{UIN: 1_000_001, Profile: profile, CurrentGameID: 7, CurrentStageGameID: 8}
	server := &Server{}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	data, err := server.newAdventureGameData(session, session.CurrentStageGameID, mapdata.AdventureMap{ID: 1602}, profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	if data.GameID != 8 || session.CurrentGameID != 7 {
		t.Fatalf("wire game ID = %d, stable match ID = %d", data.GameID, session.CurrentGameID)
	}
}

func TestNewAdventureGameDataCarriesOnlyPreparedPlayerInventory(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		game.NewPermanentItemInfo(game.SinglePlayerAdventureCardItemID, 500),
		{ItemID: 20003, NumOfItem: 3, ItemStatus: 1}, // competitive only
		{ItemID: 20020, NumOfItem: 4, ItemStatus: 1}, // competitive + adventure
		{ItemID: game.LargeStaminaPotionItemID, NumOfItem: 500, ItemStatus: 1},
	}
	selectedMap := mapdata.AdventureMap{ID: 1649, SequenceID: 12, MapIndex: 0}
	session := &connectionSession{UIN: 1_000_001, Profile: profile, CurrentGameID: 7}
	server := &Server{}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	data, err := server.newAdventureGameData(session, session.CurrentGameID, selectedMap, profile.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	if got := data.Players[0].NewItems; len(got) != 2 ||
		got[0] != (game.GameItemType{ItemID: 20020, Quantity: 4}) ||
		got[1] != (game.GameItemType{ItemID: game.LargeStaminaPotionItemID, Quantity: 500}) {
		t.Fatalf("prepared player items = %+v", got)
	}
	if len(data.NewItems) != 0 {
		t.Fatalf("prepared inventory leaked into scene elements: %+v", data.NewItems)
	}
}

func TestNewAdventureGameDataCarriesEveryParticipantsPreparedInventory(t *testing.T) {
	one := game.DefaultPlayerProfile()
	one.Inventory = []game.ItemInfo{{ItemID: game.LargeStaminaPotionItemID, NumOfItem: 11, ItemStatus: 1}}
	two := game.DefaultPlayerProfile()
	two.PlayerID = 2
	two.GameInfo.RoleID = 9
	two.Inventory = []game.ItemInfo{{ItemID: game.LargeStaminaPotionItemID, NumOfItem: 22, ItemStatus: 1}}
	actor := &connectionSession{UIN: 1_000_001, Profile: one, RoomID: 1, CurrentGameID: 7}
	peer := &connectionSession{UIN: 1_000_002, Profile: two, RoomID: 1, CurrentGameID: 7}
	actor.setUIN(actor.UIN)
	actor.setRoomID(1)
	peer.setUIN(peer.UIN)
	peer.setRoomID(1)
	actorConn, actorOther := net.Pipe()
	peerConn, peerOther := net.Pipe()
	defer actorConn.Close()
	defer actorOther.Close()
	defer peerConn.Close()
	defer peerOther.Close()
	server := &Server{liveSessions: map[net.Conn]*connectionSession{actorConn: actor, peerConn: peer}}
	participants := []match.AdventureParticipant{
		{PlayerID: one.PlayerID, RoleID: one.GameInfo.RoleID, TeamID: 1},
		{PlayerID: two.PlayerID, RoleID: two.GameInfo.RoleID, TeamID: 2},
	}
	data, err := server.newAdventureGameData(actor, actor.CurrentGameID, mapdata.AdventureMap{ID: 1649}, one.PlayerID, participants)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Players) != 2 || len(data.Players[0].NewItems) != 1 || data.Players[0].NewItems[0].Quantity != 11 || len(data.Players[1].NewItems) != 1 || data.Players[1].NewItems[0].Quantity != 22 {
		t.Fatalf("per-participant prepared items = %+v", data.Players)
	}
	for _, player := range data.Players {
		if player.TeamID != adventureCooperativeTeamID {
			t.Fatalf("adventure player %d TeamID = %d, want cooperative red team %d", player.PlayerID, player.TeamID, adventureCooperativeTeamID)
		}
	}
}

func TestAdventureWallItemsToWireRejectsUnsupportedSceneElements(t *testing.T) {
	stage := mapdata.AdventureStageRule{
		MapID:          1649,
		WallDropRolls:  1,
		WallDropChance: 100,
		WallItems: []mapdata.AdventureNPCDropItem{
			{ItemID: 20043, Quantity: 1, Kind: "material"},
		},
	}
	if _, err := adventureWallItemsToWire(stage, 0x12345678); err == nil {
		t.Fatal("unsupported client scene element was accepted")
	}
}

func TestNextGameDataSequenceSkipsZeroAfterWrap(t *testing.T) {
	server := &Server{}
	server.gameDataSeq.Store(^uint32(0))
	if got := server.nextGameDataSequence(); got != 1 {
		t.Fatalf("wrapped game-data sequence = %d, want 1", got)
	}
}
