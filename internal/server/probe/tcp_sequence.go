package probe

import (
	"fmt"
	"net"
	"time"
)

// tcpSequenceDelivery contains the legacy client's ordered response,
// notification and optional scene-transition packet. Keeping this transport
// choreography separate from dispatcher selection makes the protocol timing
// explicit without hiding packet captures or structured events.
type tcpSequenceDelivery struct {
	connection                net.Conn
	config                    ListenerConfig
	session                   *connectionSession
	connectionID              string
	local                     string
	remote                    string
	request                   []byte
	beforeResponse            []byte
	beforeResponseResult      string
	response                  []byte
	result                    string
	followUp                  []byte
	followUpResult            string
	additionalFollowUps       []tcpFollowUpPacket
	startFollowUp             []byte
	startFollowUpResult       string
	startFollowUpDelay        time.Duration
	postResponse              func()
	afterFollowUp             func()
	afterStartFollowUp        func()
	completeAdventure         bool
	completeCompetitive       bool
	adventureSettlement       *adventureSettlementCommit
	adventureRoomSettlement   *adventureRoomSettlementCommit
	competitiveRoomSettlement *competitiveRoomSettlementCommit
	finalVictory              *adventureFinalVictorySchedule
}

type tcpFollowUpPacket struct {
	data   []byte
	result string
}

func (server *Server) deliverTCPSequence(delivery tcpSequenceDelivery) bool {
	if !server.takeResponse(delivery.config) {
		return true
	}
	if len(delivery.beforeResponse) != 0 && !server.writeTCP(delivery.connection, delivery.connectionID, delivery.local, delivery.remote, delivery.beforeResponse, delivery.beforeResponseResult) {
		return server.failDeliveredTCPSequence(delivery, delivery.beforeResponseResult)
	}
	if !server.writeTCP(delivery.connection, delivery.connectionID, delivery.local, delivery.remote, delivery.response, delivery.result) {
		if delivery.postResponse != nil {
			delivery.postResponse()
		}
		return server.failDeliveredTCPSequence(delivery, delivery.result)
	}
	if delivery.postResponse != nil {
		delivery.postResponse()
	}
	if len(delivery.followUp) != 0 && !server.writeTCP(delivery.connection, delivery.connectionID, delivery.local, delivery.remote, delivery.followUp, delivery.followUpResult) {
		if delivery.afterFollowUp != nil {
			delivery.afterFollowUp()
		}
		return server.failDeliveredTCPSequence(delivery, delivery.followUpResult)
	}
	if delivery.afterFollowUp != nil {
		delivery.afterFollowUp()
	}
	completionResult := delivery.followUpResult
	for _, packet := range delivery.additionalFollowUps {
		if !server.writeTCP(delivery.connection, delivery.connectionID, delivery.local, delivery.remote, packet.data, packet.result) {
			return server.failDeliveredTCPSequence(delivery, packet.result)
		}
		completionResult = packet.result
	}
	if delivery.finalVictory != nil {
		server.scheduleAdventureFinalVictory(*delivery.finalVictory)
		return true
	}
	if len(delivery.startFollowUp) != 0 {
		server.waitStartFollowUp(delivery)
		if !server.writeTCP(delivery.connection, delivery.connectionID, delivery.local, delivery.remote, delivery.startFollowUp, delivery.startFollowUpResult) {
			if delivery.afterStartFollowUp != nil {
				delivery.afterStartFollowUp()
			}
			return server.failDeliveredTCPSequence(delivery, delivery.startFollowUpResult)
		}
		if delivery.afterStartFollowUp != nil {
			delivery.afterStartFollowUp()
		}
		completionResult = delivery.startFollowUpResult
	}
	return server.completeDeliveredMatch(delivery, completionResult)
}

func (server *Server) waitStartFollowUp(delivery tcpSequenceDelivery) {
	if delivery.startFollowUpDelay <= 0 {
		return
	}
	if delivery.competitiveRoomSettlement == nil || delivery.competitiveRoomSettlement.Battle == nil {
		time.Sleep(delivery.startFollowUpDelay)
		return
	}
	wake, active := delivery.competitiveRoomSettlement.Battle.BossVictoryGraceSignal()
	if !active {
		time.Sleep(delivery.startFollowUpDelay)
		return
	}
	timer := time.NewTimer(delivery.startFollowUpDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-wake:
	}
	roomID := uint16(0)
	if delivery.session != nil {
		roomID = uint16(delivery.session.liveRoomID.Load())
	}
	if roomID == 0 {
		roomID = sessionRoomActorID(delivery.session)
	}
	if roomID == 0 {
		return
	}
	if err := runRoomActor(server, roomID, "competitive-boss-victory-grace", func() error {
		delivery.competitiveRoomSettlement.Battle.CompleteBossVictoryGrace()
		return nil
	}); err != nil {
		server.log(logEvent{Level: "error", Event: "competitive_boss_victory_grace_actor_failed", ConnectionID: delivery.connectionID, RoomID: fmt.Sprint(roomID), ErrorContext: err.Error()})
	}
}

// failDeliveredTCPSequence closes the failed transport while still advancing
// authoritative match state. A peer disappearing during GAME_OVER delivery
// must not cancel settlement for the room or reinterpret the concluded result
// as a mid-match departure loss.
func (server *Server) failDeliveredTCPSequence(delivery tcpSequenceDelivery, result string) bool {
	if delivery.finalVictory != nil {
		server.scheduleAdventureFinalVictory(*delivery.finalVictory)
		return false
	}
	if delivery.completeAdventure || delivery.completeCompetitive {
		server.completeDeliveredMatch(delivery, result)
	}
	return false
}

func (server *Server) completeDeliveredMatch(delivery tcpSequenceDelivery, result string) bool {
	if delivery.completeCompetitive {
		if delivery.competitiveRoomSettlement == nil {
			server.log(logEvent{Level: "error", Event: "competitive_match_completion_failed", ConnectionID: delivery.connectionID, Result: result, ErrorContext: "missing competitive room settlement"})
			return false
		}
		if err := server.completeCompetitiveRoomMatch(delivery.session, delivery.connectionID, *delivery.competitiveRoomSettlement); err != nil {
			server.log(logEvent{Level: "error", Event: "competitive_match_completion_failed", ConnectionID: delivery.connectionID, Result: result, ErrorContext: err.Error()})
			return true
		}
		return true
	}
	if !delivery.completeAdventure {
		return true
	}
	var err error
	if delivery.adventureRoomSettlement != nil {
		err = server.completeAdventureRoomMatch(delivery.session, delivery.connectionID, *delivery.adventureRoomSettlement)
	} else {
		err = server.completeAdventureMatch(delivery.session, delivery.connectionID, delivery.adventureSettlement)
	}
	if err != nil {
		server.log(logEvent{Level: "error", Event: "adventure_match_completion_failed", ConnectionID: delivery.connectionID, Result: result, ErrorContext: err.Error()})
		return true
	}
	return delivery.adventureRoomSettlement != nil || server.writeAdventureSettlementInventoryRefresh(
		delivery.connection, delivery.session, delivery.connectionID, delivery.local, delivery.remote, delivery.request, delivery.adventureSettlement,
	)
}
