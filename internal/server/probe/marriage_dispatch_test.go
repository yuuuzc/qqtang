package probe

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestMarriageProposalAndAnswerUseNativeResponderOrder(t *testing.T) {
	const proposerUIN uint32 = 1_000_001
	const responderUIN uint32 = 1_000_002
	proposalPayload := binary.BigEndian.AppendUint32(nil, proposerUIN)
	proposalPayload = binary.BigEndian.AppendUint32(proposalPayload, responderUIN)
	proposalPayload = append(proposalPayload, 0)
	proposalPacket := testLocalRoutedPacketWithPayload(t, game.RequestSparkCommand, 2, 0xffff, 1, proposerUIN, proposalPayload)
	answerPayload := binary.BigEndian.AppendUint32(nil, responderUIN)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, proposerUIN)
	answerPayload = binary.BigEndian.AppendUint16(answerPayload, game.MarriageResultSuccess)
	answerPacket := testLocalRoutedPacketWithPayload(t, game.AnswerSparkCommand, 2, 0xffff, 1, responderUIN, answerPayload)

	proposerServer, proposerClient := net.Pipe()
	responderServer, responderClient := net.Pipe()
	defer proposerServer.Close()
	defer proposerClient.Close()
	defer responderServer.Close()
	defer responderClient.Close()
	proposer := newChatSession(proposerUIN, 1, 1, 0, proposerServer, proposalPacket)
	responder := newChatSession(responderUIN, 2, 1, 0, responderServer, answerPacket)
	server := newChatTestServer(t, map[net.Conn]*connectionSession{proposerServer: proposer, responderServer: responder})
	server.logWriter = io.Discard
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.playerStore = store
	proposer.Profile.Nickname = "糖一"
	proposer.Profile.Inventory = append(proposer.Profile.Inventory, game.NewPermanentItemInfo(persistence.MarriageProposalItemID, 1))
	responder.Profile.Nickname = "糖二"
	if err := store.Save(context.Background(), proposerUIN, proposer.Profile); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), responderUIN, responder.Profile); err != nil {
		t.Fatal(err)
	}

	proposalResult := server.handleMarriageMessage(proposer, "marriage-proposal", proposalPacket)
	if !proposalResult.handled || len(proposalResult.response) == 0 || proposalResult.postResponse == nil {
		t.Fatalf("proposal result = handled:%t response:%d post:%t", proposalResult.handled, len(proposalResult.response), proposalResult.postResponse != nil)
	}
	proposalInspection, err := game.InspectLocalPacket(proposalResult.response)
	if err != nil {
		t.Fatal(err)
	}
	if proposalInspection.Command != game.RequestSparkCommand || len(proposalInspection.Payload) < 11 || proposalInspection.Payload[10] == 0 {
		t.Fatalf("proposal success word is empty: command=%04X payload=%x", proposalInspection.Command, proposalInspection.Payload)
	}

	answerResult := server.handleMarriageMessage(responder, "marriage-answer", answerPacket)
	if !answerResult.handled || len(answerResult.response) == 0 || answerResult.postResponse == nil {
		t.Fatalf("answer result = handled:%t response:%d post:%t", answerResult.handled, len(answerResult.response), answerResult.postResponse != nil)
	}
	answerInspection, err := game.InspectLocalPacket(answerResult.response)
	if err != nil {
		t.Fatal(err)
	}
	if answerInspection.Command != game.AnswerSparkCommand ||
		binary.BigEndian.Uint32(answerInspection.Payload[2:6]) != proposerUIN ||
		binary.BigEndian.Uint32(answerInspection.Payload[6:10]) != responderUIN {
		t.Fatalf("answer response = command:%04X payload=%x", answerInspection.Command, answerInspection.Payload)
	}
	marriage, err := store.LoadMarriage(context.Background(), responderUIN)
	if err != nil || marriage.SpouseUIN != proposerUIN {
		t.Fatalf("accepted marriage = %+v, err=%v", marriage, err)
	}
}
