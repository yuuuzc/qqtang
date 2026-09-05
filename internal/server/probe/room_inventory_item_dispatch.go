package probe

import (
	"context"
	"fmt"
	"math"
	"time"

	"qqtang/internal/game/itemuse"
	"qqtang/internal/protocol/game"
)

// handleRoomInventoryItemMessage owns account inventory use outside a match.
// The confirmed v110 use is learning a synthesis book. Battlefield props and
// pet operations deliberately remain on their distinct protocol paths.
func (server *Server) handleRoomInventoryItemMessage(session *connectionSession, connectionID string, data []byte) loginServiceResult {
	request, err := game.DecodeLocalUseItemInRoomRequest(data)
	if err != nil {
		return loginServiceResult{keepConnection: true}
	}
	result := loginServiceResult{handled: true, keepConnection: true}
	if session.UIN == 0 || request.UIN != session.UIN {
		err = fmt.Errorf("room-item UIN %d does not match session UIN %d", request.UIN, session.UIN)
	} else if request.ItemID == 0 || request.ItemID > math.MaxUint16 {
		err = fmt.Errorf("room-item ID %d is outside inventory range", request.ItemID)
	} else if server.combineCatalog == nil {
		err = fmt.Errorf("combine recipe catalog is unavailable")
	}
	var learned game.ItemInfo
	if err == nil {
		_, found := server.combineCatalog.LookupBook(uint16(request.ItemID))
		if !found {
			err = fmt.Errorf("room item %d has no confirmed use handler", request.ItemID)
		} else {
			_, err = itemuse.Resolve(itemuse.Intent{
				Path: itemuse.PathRoomInventory, Context: sessionInventoryItemUseContext(session),
				ItemID: request.ItemID, Kind: "craft-recipe",
			})
		}
	}
	if err == nil {
		players, serviceErr := server.players()
		if serviceErr != nil {
			err = serviceErr
		} else {
			var profile game.PlayerProfile
			learned, profile, err = players.LearnCombineRecipe(context.Background(), session.UIN, server.combineCatalog, uint16(request.ItemID))
			if err == nil {
				session.replaceProfile(profile)
				server.syncPrimarySessionProfile(session, session.UIN, session.Profile)
			}
		}
	}
	resultID := uint16(0)
	if err != nil {
		resultID = 1
		result.result = "room_item_rejected"
		server.log(logEvent{
			Level: "warn", Event: "room_inventory_item_rejected", ConnectionID: connectionID,
			AccountID: fmt.Sprint(request.UIN), MessageID: fmt.Sprintf("0x%04X", game.UseItemInRoomCommand),
			Result: "rejected", ErrorContext: err.Error(),
		})
	} else {
		result.result = fmt.Sprintf("combine_book_%d_learned", request.ItemID)
	}
	result.response, err = game.BuildLocalUseItemInRoomResponse(data, resultID)
	if err != nil {
		result.response = nil
		server.log(logEvent{Level: "error", Event: "room_inventory_item_response_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		return result
	}
	if resultID == 0 {
		result.followUp, err = game.BuildLocalPlayerItemAddNotification(data, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: uint32(time.Now().Unix()), SourceUIN: session.UIN, Items: []game.ItemInfo{learned},
		})
		if err != nil {
			result.followUp = nil
			server.log(logEvent{Level: "warn", Event: "combine_book_refresh_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		} else {
			result.followUpResult = fmt.Sprintf("combine_book_%d_status_%d", request.ItemID, game.ItemStatusLearnedRecipe)
		}
	}
	return result
}
