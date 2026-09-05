package probe

import (
	"fmt"

	"qqtang/internal/protocol/game"
)

// appendPostResponse preserves the ordering of protocol-specific peer pushes
// while allowing the lobby projection to be refreshed by the same committed
// mutation. Both callbacks run only after the request acknowledgement has
// been written to the actor.
func appendPostResponse(first, second func()) func() {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return func() {
		first()
		second()
	}
}

// roomLobbyPushEntry resolves the one ROOM_INFO record currently owned by the
// application world. QQTSection derives an empty card only when the preparing
// bit is set and the packed current-player nibble is zero; a zero flag means an
// in-match card. Removed rooms therefore need that exact native tombstone.
func (server *Server) roomLobbyPushEntry(sectionID, roomID uint16, removed bool) (game.RoomListEntry, error) {
	if sectionID == 0 || roomID == 0 {
		return game.RoomListEntry{}, fmt.Errorf("room lobby push requires a section and room")
	}
	if removed {
		return game.RoomListEntry{RoomID: roomID, RoomFlag: game.RoomListFlagPreparing}, nil
	}
	snapshot, err := server.worldState().LobbySnapshot(sectionID, 0)
	if err != nil {
		return game.RoomListEntry{}, err
	}
	for _, room := range snapshot.Rooms {
		if room.RoomID == roomID {
			return roomListEntry(room)
		}
	}
	return game.RoomListEntry{}, fmt.Errorf("room %d is absent from section %d", roomID, sectionID)
}

// broadcastRoomLobbyMutation updates the native room-browser cache for every
// authenticated game connection in the section. QQT 5.2 does not periodically
// rebuild the list while the screen is open; it consumes PUSHROOMINFO (0x008C)
// deltas. Auxiliary shop sockets deliberately keep liveUIN zero and are thus
// excluded from this game-protocol broadcast.
func (server *Server) broadcastRoomLobbyMutation(sectionID, roomID uint16, removed bool, result string) {
	server.broadcastRoomLobbyMutationExcept(sectionID, roomID, removed, 0, result)
}

func (server *Server) broadcastRoomLobbyMutationExcept(sectionID, roomID uint16, removed bool, excludedUIN uint32, result string) {
	entry, err := server.roomLobbyPushEntry(sectionID, roomID, removed)
	if err != nil {
		server.log(logEvent{
			Level: "error", Event: "room_lobby_push_projection_failed", RoomID: fmt.Sprint(roomID),
			Result: result, ErrorContext: err.Error(),
		})
		return
	}

	server.liveMu.RLock()
	recipients := make([]*connectionSession, 0, len(server.liveSessions))
	for _, session := range server.liveSessions {
		uin := session.liveUIN.Load()
		if uin != 0 && uin != excludedUIN && session.liveSectionID.Load() == uint32(sectionID) {
			recipients = append(recipients, session)
		}
	}
	server.liveMu.RUnlock()

	for _, recipient := range recipients {
		template := recipient.packetTemplate()
		if len(template) == 0 {
			continue
		}
		packet, buildErr := game.BuildLocalRoomPush(template, sectionID, []game.RoomListEntry{entry})
		if buildErr != nil {
			server.log(logEvent{
				Level: "error", Event: "room_lobby_push_build_failed", ConnectionID: recipient.connectionID,
				RoomID: fmt.Sprint(roomID), Result: result, ErrorContext: buildErr.Error(),
			})
			continue
		}
		server.writeTCP(recipient.connection, recipient.connectionID, recipient.localAddress, recipient.remoteAddress, packet, result)
	}
}
