package probe

import (
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestKinChatNotifiesSenderAndOnlineKinPeer(t *testing.T) {
	const actorUIN uint32 = 1_000_001
	const peerUIN uint32 = 1_000_002
	const kinIndex uint32 = 9
	payload := binary.BigEndian.AppendUint32(nil, actorUIN)
	payload = binary.BigEndian.AppendUint32(payload, 123)
	payload = binary.BigEndian.AppendUint32(payload, kinIndex)
	payload = binary.BigEndian.AppendUint32(payload, 5)
	payload = append(payload, "hello"...)
	request := testLocalRoutedPacketWithPayload(t, game.KinChatCommand, 4, 0xffff, 1, actorUIN, payload)
	peerTemplate := testLocalRoutedPacketWithPayload(t, game.PlayerListCommand, 4, 0xffff, 1, peerUIN, make([]byte, 8))

	actorServer, actorClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer actorServer.Close()
	defer actorClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	actor := newChatSession(actorUIN, 1, 1, 0, actorServer, request)
	peer := newChatSession(peerUIN, 2, 1, 0, peerServer, peerTemplate)
	actor.Profile.KinIndex = kinIndex
	peer.Profile.KinIndex = kinIndex
	actor.liveKinIndex.Store(kinIndex)
	peer.liveKinIndex.Store(kinIndex)
	server := newChatTestServer(t, map[net.Conn]*connectionSession{actorServer: actor, peerServer: peer})
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.playerStore = store

	result := server.handleKinMessage(actor, "kin-chat", request)
	if !result.handled || len(result.response) != 0 || result.postResponse == nil {
		t.Fatalf("kin chat result = handled:%t response:%d post:%t", result.handled, len(result.response), result.postResponse != nil)
	}
	actorRead, peerRead := readChatPacket(actorClient), readChatPacket(peerClient)
	result.postResponse()
	for label, read := range map[string]<-chan chatPacketRead{"self": actorRead, "peer": peerRead} {
		got := <-read
		if got.err != nil {
			t.Fatalf("%s read: %v", label, got.err)
		}
		inspection, inspectErr := game.InspectLocalPacket(got.packet)
		if inspectErr != nil {
			t.Fatalf("%s inspect: %v", label, inspectErr)
		}
		if inspection.Command != game.KinChatNotifyCommand ||
			binary.BigEndian.Uint32(inspection.Payload[0:4]) != actorUIN ||
			binary.BigEndian.Uint32(inspection.Payload[4:8]) != kinIndex ||
			binary.BigEndian.Uint32(inspection.Payload[8:12]) != 5 ||
			string(inspection.Payload[12:]) != "hello" {
			t.Fatalf("%s kin chat notification = %+v payload=%x", label, inspection, inspection.Payload)
		}
	}
}
