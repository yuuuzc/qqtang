package probe

import (
	"fmt"

	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

const competitiveAIUINBase uint32 = 1_900_000_000

// competitiveAIRoomProjection is the client-side identity required before a
// virtual participant can appear in GAME_BEGIN. It is deliberately not a room
// member: ownership, readiness, lobby population and arbitration remain human
// server state. The matching leave notification removes this temporary client
// object after GAME_OVER.
type competitiveAIRoomProjection struct {
	UIN      uint32
	PlayerID uint16
	RoleID   byte
	TeamID   byte
	SeatID   byte
	Nickname string
}

func buildCompetitiveAIRoomProjections(snapshot roomstate.Snapshot, participants []match.CompetitiveParticipant) ([]competitiveAIRoomProjection, error) {
	if len(participants) == 0 {
		return nil, nil
	}
	occupied := make(map[byte]struct{}, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if member.SeatID == 0 || member.SeatID > roomstate.RoomSeatCount {
			return nil, fmt.Errorf("room %d member %d has invalid seat %d", snapshot.RoomID, member.PlayerID, member.SeatID)
		}
		occupied[member.SeatID] = struct{}{}
	}
	freeSeats := make([]byte, 0, roomstate.RoomSeatCount-len(snapshot.Members))
	for seatID := byte(1); seatID <= roomstate.RoomSeatCount; seatID++ {
		if snapshot.LockedSeats[seatID-1] {
			continue
		}
		if _, used := occupied[seatID]; used {
			continue
		}
		freeSeats = append(freeSeats, seatID)
	}
	if len(freeSeats) < len(participants) {
		return nil, fmt.Errorf("room %d has %d free seats for %d virtual participants", snapshot.RoomID, len(freeSeats), len(participants))
	}
	result := make([]competitiveAIRoomProjection, len(participants))
	for index, participant := range participants {
		if participant.Source != match.CompetitiveParticipantVirtualAI {
			return nil, fmt.Errorf("room projection participant %d is not virtual AI", participant.PlayerID)
		}
		result[index] = competitiveAIRoomProjection{
			UIN: competitiveAIUINBase + uint32(participant.PlayerID), PlayerID: participant.PlayerID,
			RoleID: participant.RoleID, TeamID: participant.TeamID, SeatID: freeSeats[index],
			Nickname: fmt.Sprintf("AI%d", index+1),
		}
	}
	return result, nil
}

func (projection competitiveAIRoomProjection) enterNotification(roomID uint16) (game.EnterRoomNotificationOld, error) {
	if roomID == 0 || projection.UIN == 0 || projection.PlayerID == 0 || projection.RoleID == 0 || projection.TeamID == 0 || projection.SeatID == 0 {
		return game.EnterRoomNotificationOld{}, fmt.Errorf("competitive AI room projection is incomplete")
	}
	profile := game.PlayerProfile{
		Nickname: projection.Nickname, PlayerID: projection.PlayerID, SectionID: 1,
		MinimumRoomID: 1, GameInfo: game.GameInfo{RoleID: projection.RoleID, Degree: 1},
	}
	player := game.PlayerInfoInRoomOldFromProfile(projection.UIN, profile, projection.TeamID, projection.SeatID, 0)
	return game.EnterRoomNotificationOld{Player: player, RoomID: roomID}, nil
}

func (server *Server) broadcastCompetitiveAIEnterProjections(roomID uint16, projections []competitiveAIRoomProjection) {
	for _, projection := range projections {
		notification, err := projection.enterNotification(roomID)
		if err != nil {
			server.log(logEvent{Level: "error", Event: "competitive_ai_room_enter_build_failed", RoomID: fmt.Sprint(roomID), ErrorContext: err.Error()})
			continue
		}
		server.broadcastRoomNotification(roomID, 0, fmt.Sprintf("qqt_competitive_ai_player_%d_enter", projection.PlayerID), func(recipient []byte) ([]byte, error) {
			return game.BuildLocalEnterRoomNotification(recipient, notification)
		})
	}
}

func (server *Server) broadcastCompetitiveAILeaveProjections(roomID uint16, gameID uint32, roomOwnerID, arbitratorID uint16) {
	runtime := server.competitiveAIRuntimeForGame(gameID)
	if runtime == nil || len(runtime.roomProjections) == 0 {
		return
	}
	mapID := runtime.mapID
	if mapID > uint32(^uint16(0)) {
		server.log(logEvent{Level: "error", Event: "competitive_ai_room_leave_build_failed", RoomID: fmt.Sprint(roomID), Result: fmt.Sprintf("game_%d_map_%d", gameID, mapID), ErrorContext: "map ID exceeds leave-room field"})
		return
	}
	projections := append([]competitiveAIRoomProjection(nil), runtime.roomProjections...)
	for _, projection := range projections {
		leave := game.LeaveRoomNotification{
			PlayerID: projection.PlayerID, NewRoomOwnerID: roomOwnerID,
			NewArbitratorID: arbitratorID, MapID: uint16(mapID),
		}
		server.broadcastRoomNotification(roomID, 0, fmt.Sprintf("qqt_competitive_ai_player_%d_leave", projection.PlayerID), func(recipient []byte) ([]byte, error) {
			return game.BuildLocalLeaveRoomNotification(recipient, roomID, leave)
		})
	}
}
