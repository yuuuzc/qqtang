package probe

import (
	"context"
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestKinInvitationUsesNativeServerOperationRoundTrip(t *testing.T) {
	const inviterUIN uint32 = 1_000_001
	const responderUIN uint32 = 1_000_002
	ctx := context.Background()
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	inviterProfile := game.DefaultPlayerProfile()
	inviterProfile.Nickname = "糖一"
	inviterProfile.Inventory = append(inviterProfile.Inventory, game.NewPermanentItemInfo(persistence.KinSuperAllianceBookItemID, 1))
	responderProfile := game.DefaultPlayerProfile()
	responderProfile.Nickname = "糖二"
	if err := store.Save(ctx, inviterUIN, inviterProfile); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, responderUIN, responderProfile); err != nil {
		t.Fatal(err)
	}
	kin, err := store.CreateKin(ctx, inviterUIN, "糖家族", "欢迎", 0x10, 1)
	if err != nil {
		t.Fatal(err)
	}

	invitePayload := binary.BigEndian.AppendUint32(nil, inviterUIN)
	invitePayload = binary.BigEndian.AppendUint32(invitePayload, 123)
	invitePayload = binary.BigEndian.AppendUint32(invitePayload, kin.Index)
	invitePayload = binary.BigEndian.AppendUint32(invitePayload, responderUIN)
	invitePayload = binary.BigEndian.AppendUint32(invitePayload, game.KinOperationAccepted)
	invitePayload = binary.BigEndian.AppendUint32(invitePayload, 0)
	invitePacket := testLocalRoutedPacketWithPayload(t, game.OperateKinCommand, 4, 0xffff, 1, inviterUIN, invitePayload)

	answerPayload := binary.BigEndian.AppendUint32(nil, inviterUIN)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, 124)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, kin.Index)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, responderUIN)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, game.KinOperationAccepted)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, 0)
	answerPacket := testLocalRoutedPacketWithPayload(t, game.ServerOperateKinCommand, 4, 0xffff, 1, responderUIN, answerPayload)

	inviterServer, inviterClient := net.Pipe()
	responderServer, responderClient := net.Pipe()
	defer inviterServer.Close()
	defer inviterClient.Close()
	defer responderServer.Close()
	defer responderClient.Close()
	inviter := newChatSession(inviterUIN, 1, 1, 0, inviterServer, invitePacket)
	responder := newChatSession(responderUIN, 2, 1, 0, responderServer, answerPacket)
	inviter.Profile = inviterProfile
	inviter.Profile.KinIndex = kin.Index
	inviter.liveKinIndex.Store(kin.Index)
	responder.Profile = responderProfile
	server := newChatTestServer(t, map[net.Conn]*connectionSession{inviterServer: inviter, responderServer: responder})
	server.playerStore = store

	inviteResult := server.handleKinMessage(inviter, "kin-invite", invitePacket)
	if !inviteResult.handled || len(inviteResult.response) == 0 || inviteResult.postResponse == nil {
		t.Fatalf("invite result = handled:%t response:%d post:%t", inviteResult.handled, len(inviteResult.response), inviteResult.postResponse != nil)
	}
	responseInspection, err := game.InspectLocalPacket(inviteResult.response)
	if err != nil {
		t.Fatal(err)
	}
	if responseInspection.Command != game.OperateKinCommand || binary.BigEndian.Uint32(responseInspection.Payload[16:20]) != game.KinOperationAccepted {
		t.Fatalf("invite response = command:%04x payload:%x", responseInspection.Command, responseInspection.Payload)
	}
	if len(responseInspection.Payload) != 26 || binary.BigEndian.Uint16(responseInspection.Payload[24:26]) != 0 {
		t.Fatalf("successful invite response must keep ReasonDesStr empty: %x", responseInspection.Payload)
	}
	targetRead := readChatPacket(responderClient)
	inviteResult.postResponse()
	targetPacket := <-targetRead
	if targetPacket.err != nil {
		t.Fatal(targetPacket.err)
	}
	targetInspection, err := game.InspectLocalPacket(targetPacket.packet)
	if err != nil {
		t.Fatal(err)
	}
	if targetInspection.Command != game.ServerOperateKinCommand ||
		binary.BigEndian.Uint32(targetInspection.Payload[0:4]) != inviterUIN ||
		binary.BigEndian.Uint32(targetInspection.Payload[8:12]) != kin.Index ||
		binary.BigEndian.Uint32(targetInspection.Payload[12:16]) != responderUIN {
		t.Fatalf("target invitation = command:%04x payload:%x", targetInspection.Command, targetInspection.Payload)
	}

	answerResult := server.handleKinMessage(responder, "kin-answer", answerPacket)
	if !answerResult.handled || len(answerResult.response) != 0 || len(answerResult.followUp) == 0 {
		t.Fatalf("answer result = handled:%t response:%d follow:%d", answerResult.handled, len(answerResult.response), len(answerResult.followUp))
	}
	answerInspection, err := game.InspectLocalPacket(answerResult.followUp)
	if err != nil {
		t.Fatal(err)
	}
	if answerInspection.Command != game.ServerOperateKinNotifyCommand ||
		binary.BigEndian.Uint32(answerInspection.Payload[16:20]) != game.KinOperationAccepted {
		t.Fatalf("answer notification = command:%04x payload:%x", answerInspection.Command, answerInspection.Payload)
	}
	profile, err := store.Load(ctx, responderUIN)
	if err != nil || profile.KinIndex != kin.Index {
		t.Fatalf("accepted responder profile = %+v, err=%v", profile, err)
	}
}
