package probe

import (
	"encoding/binary"
	"fmt"

	"qqtang/internal/protocol/game"
)

// handlePeerTransportMessage owns the TCP control plane that establishes the
// QQTPPP room-peer UDP path. Real-time datagrams themselves remain in
// room_peer_udp.go; forwarding the 0x0086 request as a UDP Type-3 packet is not
// protocol-correct.
func (server *Server) handlePeerTransportMessage(session *connectionSession, connectionID string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil || inspection.Command != game.RequestUDPOKCommand {
		return protocolMessageResult{}
	}
	request, err := game.DecodeLocalTransferUDPOKRequest(data)
	if err == nil {
		if virtual, handled := server.handleCompetitiveAIVirtualUDPOK(session, request); handled {
			return virtual
		}
	}
	if err == nil {
		err = server.validateTransferUDPOKRequest(session, request)
	}
	if err != nil {
		server.log(logEvent{
			Level: "warn", Event: "peer_udp_ok_rejected", ConnectionID: connectionID,
			AccountID: fmt.Sprint(session.UIN), MessageID: fmt.Sprintf("0x%04X", game.RequestUDPOKCommand),
			Result: "rejected", ErrorContext: err.Error(),
		})
		return protocolMessageResult{handled: true, result: "qqt_peer_udp_ok_rejected"}
	}
	notification := game.TransferUDPOKNotification{
		RecipientUIN: request.DestinationUIN, ClientTime: request.ClientTime,
		SourcePlayerID: session.Profile.PlayerID, SourceUIN: request.SourceUIN,
		Info: append([]byte(nil), request.Info...),
	}
	return protocolMessageResult{
		handled: true,
		result:  fmt.Sprintf("qqt_peer_udp_ok_request_to_%d", request.DestinationUIN),
		postResponse: func() {
			server.broadcastDirectNotification(
				request.SourceUIN,
				request.DestinationUIN,
				fmt.Sprintf("qqt_peer_udp_ok_notify_from_%d", request.SourceUIN),
				func(recipient []byte) ([]byte, error) {
					return game.BuildLocalTransferUDPOKNotificationForRecipient(recipient, notification)
				},
			)
		},
	}
}

const competitiveAIPeerTransportRoute uint16 = 3

// handleCompetitiveAIVirtualUDPOK supplies the half of QQTPPP's native peer
// negotiation normally emitted by a second client. The request from the human
// is still decoded and authenticated exactly like a real 0x0086; only its
// destination is a server-owned room projection instead of a live TCP session.
func (server *Server) handleCompetitiveAIVirtualUDPOK(session *connectionSession, request game.TransferUDPOKRequest) (protocolMessageResult, bool) {
	if session == nil || session.UIN == 0 || request.SourceUIN != session.UIN || session.liveUIN.Load() != session.UIN {
		return protocolMessageResult{}, false
	}
	gameID := session.gameplayGameID()
	runtime := server.competitiveAIRuntimeForGame(gameID)
	if runtime == nil || runtime.roomID == 0 || session.liveRoomID.Load() != uint32(runtime.roomID) {
		return protocolMessageResult{}, false
	}
	projection, virtual := runtime.virtualProjection(request.DestinationPlayerID, request.DestinationUIN)
	if !virtual {
		return protocolMessageResult{}, false
	}
	runtime.mu.Lock()
	_, human := runtime.humanIDs[session.routingPlayerID()]
	runtime.mu.Unlock()
	if !human {
		return protocolMessageResult{handled: true, result: "qqt_competitive_ai_udp_ok_rejected"}, true
	}

	info := []byte{game.TransferUDPOKStartNegotiationMode}
	complete := len(request.Info) != 0 && request.Info[0] != game.TransferUDPOKStartNegotiationMode
	if complete {
		// A native non-zero descriptor names the target peer followed by its
		// room transport route. For the reciprocal virtual response the target
		// is therefore the requesting human, not the AI identity in request.Info.
		info = make([]byte, 7)
		info[0] = 1
		binary.BigEndian.PutUint32(info[1:5], request.SourceUIN)
		binary.BigEndian.PutUint16(info[5:7], competitiveAIPeerTransportRoute)
	}
	notification := game.TransferUDPOKNotification{
		RecipientUIN: request.SourceUIN, ClientTime: request.ClientTime,
		SourcePlayerID: projection.PlayerID, SourceUIN: projection.UIN, Info: info,
	}
	return protocolMessageResult{
		handled: true,
		result:  fmt.Sprintf("qqt_competitive_ai_udp_ok_from_%d", projection.PlayerID),
		postResponse: func() {
			server.sendUINNotification(request.SourceUIN, fmt.Sprintf("qqt_competitive_ai_udp_ok_notify_from_%d", projection.PlayerID), func(recipient []byte) ([]byte, error) {
				return game.BuildLocalTransferUDPOKNotificationForRecipient(recipient, notification)
			})
			if complete {
				// postResponse runs after the dispatcher has released both session.mu
				// and the room actor, so the state transition must re-enter the room
				// mailbox instead of mutating the AI runtime from the socket goroutine.
				server.markCompetitiveAINativePeerReady(runtime, session.routingPlayerID(), projection.PlayerID)
			}
		},
	}, true
}

func (server *Server) validateTransferUDPOKRequest(session *connectionSession, request game.TransferUDPOKRequest) error {
	if session == nil || session.UIN == 0 || session.liveUIN.Load() != session.UIN {
		return fmt.Errorf("sender has no authenticated live session")
	}
	if request.SourceUIN != session.UIN {
		return fmt.Errorf("source UIN %d does not match session UIN %d", request.SourceUIN, session.UIN)
	}
	roomID := session.liveRoomID.Load()
	if roomID == 0 {
		return fmt.Errorf("sender is not in a room")
	}
	if request.DestinationUIN == session.UIN {
		return fmt.Errorf("destination UIN equals sender UIN")
	}

	server.liveMu.RLock()
	var destination *connectionSession
	for _, target := range server.liveSessions {
		if target.liveUIN.Load() != request.DestinationUIN {
			continue
		}
		destination = target
		break
	}
	server.liveMu.RUnlock()
	if destination == nil {
		return fmt.Errorf("destination UIN %d has no live session", request.DestinationUIN)
	}
	if destination.liveUIN.Load() != request.DestinationUIN || destination.liveRoomID.Load() != roomID {
		return fmt.Errorf("destination UIN %d is not in sender room %d", request.DestinationUIN, roomID)
	}
	destinationPlayerID := destination.routingPlayerID()
	if destinationPlayerID != request.DestinationPlayerID {
		return fmt.Errorf("destination UIN %d player ID %d, request has %d", request.DestinationUIN, destinationPlayerID, request.DestinationPlayerID)
	}
	return nil
}
