package probe

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/game/equipment"
	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestFriendDispatcherRejectsAnotherAuthenticatedUIN(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	payload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	payload = binary.BigEndian.AppendUint32(payload, 77)
	packet := testLocalRoutedPacketWithPayload(t, game.FriendListCommand, 2, 0xffff, 1, 1_000_001, payload)
	server := &Server{playerStore: store, logWriter: io.Discard}
	result := server.handleFriendMessage(&connectionSession{UIN: 1_000_002}, "friend-spoof", packet)
	if !result.handled || len(result.response) != 0 {
		t.Fatalf("spoofed friend request result = %+v", result)
	}
}

func TestFriendDispatcherReturnsCanonicalFixedList(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const ownerUIN uint32 = 1_000_001
	const friendUIN uint32 = 1_000_002
	if err := store.Save(context.Background(), ownerUIN, game.DefaultPlayerProfile()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), friendUIN, game.DefaultPlayerProfile()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestFriend(context.Background(), ownerUIN, friendUIN, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AnswerFriendRequest(context.Background(), friendUIN, ownerUIN, true); err != nil {
		t.Fatal(err)
	}
	payload := binary.BigEndian.AppendUint32(nil, ownerUIN)
	payload = binary.BigEndian.AppendUint32(payload, 77)
	packet := testLocalRoutedPacketWithPayload(t, game.FriendListCommand, 2, 0xffff, 1, ownerUIN, payload)
	server := &Server{playerStore: store, logWriter: io.Discard}
	result := server.handleFriendMessage(&connectionSession{UIN: ownerUIN}, "friend-list", packet)
	if !result.handled || len(result.response) == 0 {
		t.Fatalf("friend list result = %+v", result)
	}
	inspection, err := game.InspectLocalPacket(result.response)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Payload) != game.FriendListPayloadSize || binary.BigEndian.Uint16(inspection.Payload[8:10]) != 1 || binary.BigEndian.Uint32(inspection.Payload[10:14]) != friendUIN {
		t.Fatalf("friend list payload len=%d body=%x", len(inspection.Payload), inspection.Payload[:18])
	}
}

func TestFriendProjectionUsesLiveSelectedRole(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const friendUIN uint32 = 1_000_002
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.RoleID = 0 // selected role is intentionally not durable
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(22, 1)}
	if err := store.Save(context.Background(), friendUIN, profile); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), friendUIN, []equipment.Change{{
		RoleID: 7, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true,
	}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	live := &connectionSession{UIN: friendUIN, SelectedRoleID: 7, Profile: profile}
	live.liveUIN.Store(friendUIN)
	server := &Server{
		playerStore: store, equipmentCatalog: catalog, logWriter: io.Discard,
		liveSessions: map[net.Conn]*connectionSession{nil: live},
	}
	update, err := server.friendUpdateProjection(1_000_001, friendUIN, live)
	if err != nil {
		t.Fatal(err)
	}
	if !update.Online || len(update.ExtItemIDs) != 1 || update.ExtItemIDs[0] != 22 {
		t.Fatalf("friend projection = %+v", update)
	}
}

func TestFriendSessionRestoreHydratesDurableFriendWithoutWaitingForPresenceEdge(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const ownerUIN uint32 = 1_000_001
	const friendUIN uint32 = 1_000_002
	ownerProfile := game.DefaultPlayerProfile()
	friendProfile := game.DefaultPlayerProfile()
	friendProfile.Nickname = "糖二"
	if err := store.Save(context.Background(), ownerUIN, ownerProfile); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), friendUIN, friendProfile); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestFriend(context.Background(), ownerUIN, friendUIN, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AnswerFriendRequest(context.Background(), friendUIN, ownerUIN, true); err != nil {
		t.Fatal(err)
	}
	liveFriend := &connectionSession{UIN: friendUIN, Profile: friendProfile}
	liveFriend.liveUIN.Store(friendUIN)
	server := &Server{playerStore: store, logWriter: io.Discard, liveSessions: map[net.Conn]*connectionSession{nil: liveFriend}}
	payload := binary.BigEndian.AppendUint32(nil, ownerUIN)
	payload = binary.BigEndian.AppendUint32(payload, 77)
	template := testLocalRoutedPacketWithPayload(t, game.FriendListCommand, 2, 0xffff, 1, ownerUIN, payload)
	packets := server.friendSessionRestorePackets(template, ownerUIN, &connectionSession{UIN: ownerUIN, Profile: ownerProfile}, "friend-restore")
	if len(packets) != 2 || packets[0].result != "qqt_friend_session_restore_roster" || packets[1].result != "qqt_friend_session_restore_1000002" {
		t.Fatalf("friend restore packets = %+v", packets)
	}
	roster, err := game.InspectLocalPacket(packets[0].data)
	if err != nil {
		t.Fatal(err)
	}
	if roster.Command != game.FriendListCommand || binary.BigEndian.Uint16(roster.Payload[8:10]) != 1 || binary.BigEndian.Uint32(roster.Payload[10:14]) != friendUIN {
		t.Fatalf("friend restore roster = %+v payload=%x", roster, roster.Payload)
	}
	inspection, err := game.InspectLocalPacket(packets[1].data)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.FriendUpdateNotifyCommand || inspection.EnvelopeUIN != ownerUIN ||
		binary.BigEndian.Uint32(inspection.Payload[0:4]) != ownerUIN ||
		binary.BigEndian.Uint32(inspection.Payload[4:8]) != friendUIN || inspection.Payload[133] != 1 {
		t.Fatalf("friend restore notification = %+v payload=%x", inspection, inspection.Payload)
	}
}

func TestCompactAddFriendRequestDeliversNativeRequestToTarget(t *testing.T) {
	const requesterUIN uint32 = 1_000_001
	const targetUIN uint32 = 1_000_002
	// Native DoFriendTask prefixes the free-form greeting with a fixed
	// NUL-padded sender-name slot. Preserve those embedded NULs when forwarding
	// the request; they delimit the envelope's displayed sender and message.
	word := []byte{0xcc, 0xc7, 0xd2, 0xbb} // 糖一 (GBK)
	word = append(word, make([]byte, 17)...)
	word = append(word, 0xbc, 0xd3, 0xce, 0xd2) // 加我 (GBK)
	payload := binary.BigEndian.AppendUint32(nil, requesterUIN)
	payload = binary.BigEndian.AppendUint32(payload, 88)
	payload = binary.BigEndian.AppendUint32(payload, targetUIN)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(word)))
	payload = append(payload, word...)
	request := testLocalRoutedPacketWithPayload(t, game.AddFriendCommand, 2, 0xffff, 1, requesterUIN, payload)
	targetTemplatePayload := binary.BigEndian.AppendUint32(nil, targetUIN)
	targetTemplatePayload = binary.BigEndian.AppendUint32(targetTemplatePayload, 99)
	targetTemplate := testLocalRoutedPacketWithPayload(t, game.FriendListCommand, 2, 0xffff, 1, targetUIN, targetTemplatePayload)

	requesterServer, requesterClient := net.Pipe()
	targetServer, targetClient := net.Pipe()
	defer requesterServer.Close()
	defer requesterClient.Close()
	defer targetServer.Close()
	defer targetClient.Close()
	requester := newChatSession(requesterUIN, 1, 1, 0, requesterServer, request)
	requester.Profile.Nickname = "糖一"
	target := newChatSession(targetUIN, 2, 1, 0, targetServer, targetTemplate)
	target.Profile.Nickname = "糖二"
	server := newChatTestServer(t, map[net.Conn]*connectionSession{requesterServer: requester, targetServer: target})
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.playerStore = store
	if err = store.Save(context.Background(), requesterUIN, requester.Profile); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(context.Background(), targetUIN, target.Profile); err != nil {
		t.Fatal(err)
	}

	result := server.handleFriendMessage(requester, "friend-add", request)
	if !result.handled || len(result.response) != 0 || result.postResponse == nil {
		t.Fatalf("add friend result = handled:%t response:%d post:%t", result.handled, len(result.response), result.postResponse != nil)
	}
	targetRead := readChatPacket(targetClient)
	result.postResponse()
	got := <-targetRead
	if got.err != nil {
		t.Fatal(got.err)
	}
	inspection, err := game.InspectLocalPacket(got.packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.AddFriendCommand || inspection.EnvelopeUIN != targetUIN ||
		inspection.RouteSequence != 0 || inspection.InnerSequence != 0 ||
		len(inspection.Payload) != game.AddFriendHeaderSize+len(word) ||
		binary.BigEndian.Uint32(inspection.Payload[0:4]) != requesterUIN ||
		binary.BigEndian.Uint32(inspection.Payload[8:12]) != targetUIN ||
		binary.BigEndian.Uint16(inspection.Payload[12:14]) != uint16(len(word)) ||
		string(inspection.Payload[14:]) != string(word) {
		t.Fatalf("add-friend target notification = %+v payload=%x", inspection, inspection.Payload)
	}
}
