package probe

import (
	"fmt"
	"math"

	"qqtang/internal/game/itemuse"
	roomstate "qqtang/internal/game/room"
)

func matchItemUseContext(category roomstate.Category) (itemuse.Context, error) {
	switch category {
	case roomstate.CategoryAdventure:
		return itemuse.ContextAdventure, nil
	case roomstate.CategoryCompetitive:
		return itemuse.ContextCompetitive, nil
	default:
		return "", fmt.Errorf("room category %d has no match item-use context", category)
	}
}

func (server *Server) resolveMatchItemUse(session *connectionSession, path itemuse.Path, itemID uint32) (itemuse.Plan, error) {
	if session == nil || session.CurrentGameID == 0 || session.Profile.PlayerID == 0 {
		return itemuse.Plan{}, fmt.Errorf("match item use requires an active player session")
	}
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return itemuse.Plan{}, err
	}
	context, err := matchItemUseContext(category)
	if err != nil {
		return itemuse.Plan{}, err
	}
	if path == itemuse.PathPreparedProp && category == roomstate.CategoryCompetitive {
		room, roomErr := server.sessionRoom(session)
		if roomErr != nil {
			return itemuse.Plan{}, roomErr
		}
		field, competitive := room.Snapshot().Settings.GameType.CompetitiveField()
		if !competitive || field != roomstate.CompetitiveFieldItem {
			return itemuse.Plan{}, fmt.Errorf("prepared inventory prop %d requires a competitive item room", itemID)
		}
	}
	kind := ""
	if itemID <= math.MaxUint16 && server.itemKindsByID != nil {
		kind = server.itemKindsByID[uint16(itemID)]
	}
	return itemuse.Resolve(itemuse.Intent{Path: path, Context: context, ItemID: itemID, Kind: kind})
}

func sessionInventoryItemUseContext(session *connectionSession) itemuse.Context {
	if session != nil && session.RoomID != 0 {
		return itemuse.ContextRoom
	}
	return itemuse.ContextLobby
}
