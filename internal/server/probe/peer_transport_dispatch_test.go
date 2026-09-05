package probe

import (
	"net"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestValidateTransferUDPOKRequestRequiresExactRoomPeer(t *testing.T) {
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession)}
	actor := &connectionSession{UIN: 1_000_002, Profile: game.PlayerProfile{PlayerID: 2}}
	target := &connectionSession{UIN: 1_000_001, Profile: game.PlayerProfile{PlayerID: 1}}
	actor.liveUIN.Store(actor.UIN)
	target.liveUIN.Store(target.UIN)
	actor.liveRoomID.Store(7)
	target.liveRoomID.Store(7)
	actorConnection, actorPeer := net.Pipe()
	targetConnection, targetPeer := net.Pipe()
	defer actorConnection.Close()
	defer actorPeer.Close()
	defer targetConnection.Close()
	defer targetPeer.Close()
	server.liveSessions[actorConnection] = actor
	server.liveSessions[targetConnection] = target

	request := game.TransferUDPOKRequest{
		SourceUIN: actor.UIN, DestinationUIN: target.UIN,
		DestinationPlayerID: target.Profile.PlayerID, Info: []byte{0},
	}
	if err := server.validateTransferUDPOKRequest(actor, request); err != nil {
		t.Fatal(err)
	}
	request.DestinationPlayerID++
	if err := server.validateTransferUDPOKRequest(actor, request); err == nil {
		t.Fatal("mismatched destination player ID was accepted")
	}
	target.liveRoomID.Store(8)
	request.DestinationPlayerID = target.Profile.PlayerID
	if err := server.validateTransferUDPOKRequest(actor, request); err == nil {
		t.Fatal("destination in another room was accepted")
	}
}
