package probe

import (
	"fmt"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func (server *Server) handleChatBackgroundMessage(session *connectionSession, connectionID string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil || inspection.Command != game.BackgroundListCommand && inspection.Command != game.UseBackgroundCommand && inspection.Command != game.UseBackgroundNotifyCommand {
		return protocolMessageResult{}
	}
	if inspection.Command == game.UseBackgroundNotifyCommand {
		return server.rejectedChatBackground(connectionID, inspection.Command, fmt.Errorf("client originated server-only background notification"))
	}
	if session == nil || session.UIN == 0 || session.RoomID == 0 {
		return server.rejectedChatBackground(connectionID, inspection.Command, fmt.Errorf("authenticated room session is unavailable"))
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		return server.rejectedChatBackground(connectionID, inspection.Command, err)
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhasePreparing || snapshot.Settings.GameType != roomstate.GameTypeChat {
		return server.rejectedChatBackground(connectionID, inspection.Command, fmt.Errorf("room %d is not a preparing chat room", snapshot.RoomID))
	}

	switch inspection.Command {
	case game.BackgroundListCommand:
		request, decodeErr := game.DecodeLocalBackgroundListRequest(data)
		if decodeErr != nil {
			return server.rejectedChatBackground(connectionID, inspection.Command, decodeErr)
		}
		response, buildErr := game.BuildLocalBackgroundListResponse(data, game.BuiltInChatBackgroundIDs())
		if buildErr != nil {
			return server.rejectedChatBackground(connectionID, inspection.Command, buildErr)
		}
		return protocolMessageResult{handled: true, response: response, result: fmt.Sprintf("qqt_chat_background_list_%d", request.UIN)}

	case game.UseBackgroundCommand:
		request, decodeErr := game.DecodeLocalUseBackgroundRequest(data)
		if decodeErr != nil {
			return server.rejectedChatBackground(connectionID, inspection.Command, decodeErr)
		}
		resultID := game.UseBackgroundResultSuccess
		var operationErr error
		if !game.IsBuiltInChatBackgroundID(request.ItemID) {
			operationErr = fmt.Errorf("chat background ID %d is absent from the shipped client catalog", request.ItemID)
		} else {
			_, operationErr = server.worldState().SetChatBackground(sessionWorldUIN(session), request.ItemID)
		}
		if operationErr != nil {
			resultID = game.UseBackgroundResultFailed
		}
		response, buildErr := game.BuildLocalUseBackgroundResponse(data, resultID)
		if buildErr != nil {
			return server.rejectedChatBackground(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			server.log(logEvent{Level: "warn", Event: "chat_background_change_rejected", ConnectionID: connectionID, AccountID: fmt.Sprint(request.UIN), RoomID: fmt.Sprint(snapshot.RoomID), MessageID: fmt.Sprintf("0x%04X", inspection.Command), Result: "rejected", ErrorContext: operationErr.Error()})
			return protocolMessageResult{handled: true, response: response, result: "qqt_chat_background_rejected"}
		}
		roomID := snapshot.RoomID
		weddingMode, weddingBackground := game.ChatBackgroundWeddingMode(request.ItemID)
		var followUp []byte
		followUpResult := ""
		if weddingBackground {
			followUp, buildErr = game.BuildLocalChangeWeddingModeNotification(data, request.UIN, weddingMode)
			if buildErr != nil {
				return server.rejectedChatBackground(connectionID, inspection.Command, buildErr)
			}
			followUpResult = fmt.Sprintf("qqt_chat_wedding_background_%d_mode_%d", request.ItemID, weddingMode)
		}
		return protocolMessageResult{
			handled: true, response: response, result: fmt.Sprintf("qqt_chat_background_%d", request.ItemID),
			followUp: followUp, followUpResult: followUpResult,
			postResponse: func() {
				server.broadcastRoomNotification(roomID, request.UIN, "qqt_chat_background_peer", func(recipient []byte) ([]byte, error) {
					return game.BuildLocalUseBackgroundNotification(recipient, roomID, request)
				})
				if weddingBackground {
					server.broadcastRoomNotification(roomID, request.UIN, "qqt_chat_wedding_background_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalChangeWeddingModeNotification(recipient, request.UIN, weddingMode)
					})
				}
			},
		}
	}
	return protocolMessageResult{}
}

func (server *Server) rejectedChatBackground(connectionID string, command uint16, err error) protocolMessageResult {
	server.log(logEvent{Level: "warn", Event: "chat_background_rejected", ConnectionID: connectionID, MessageID: fmt.Sprintf("0x%04X", command), Result: "rejected", ErrorContext: err.Error()})
	return protocolMessageResult{handled: true}
}
