package probe

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"time"

	"qqtang/internal/game/craftcatalog"
	"qqtang/internal/protocol/game"
)

func (server *Server) handleCraftMessage(session *connectionSession, connectionID string, data []byte) loginServiceResult {
	if request, err := game.DecodeLocalForgeRequest(data); err == nil {
		return server.handleAvatarForge(session, connectionID, data, request)
	}
	if request, err := game.DecodeLocalCombineForgeRequest(data); err == nil {
		return server.handleCombine(session, connectionID, data, request)
	}
	return loginServiceResult{keepConnection: true}
}

func (server *Server) handleCombine(session *connectionSession, connectionID string, data []byte, request game.CombineForgeRequest) loginServiceResult {
	result := loginServiceResult{handled: true, keepConnection: true}
	var combineErr error
	if session.UIN == 0 || request.UIN != session.UIN {
		combineErr = fmt.Errorf("combine UIN %d does not match session UIN %d", request.UIN, session.UIN)
	} else if request.Type != 0 {
		combineErr = fmt.Errorf("combine operation type %d is unsupported", request.Type)
	} else if request.ItemID == 0 || request.ItemID > math.MaxUint16 || request.FromItemID != 0 {
		combineErr = fmt.Errorf("combine product/from item IDs %d/%d are invalid", request.ItemID, request.FromItemID)
	} else if server.combineCatalog == nil {
		combineErr = fmt.Errorf("combine recipe catalog is unavailable")
	}
	var changes []game.ItemInfo
	if combineErr == nil {
		players, serviceErr := server.players()
		if serviceErr != nil {
			combineErr = serviceErr
		} else {
			combined, serviceErr := players.Combine(context.Background(), session.UIN, server.combineCatalog, uint16(request.ItemID), request.MaterialIDs)
			if serviceErr != nil {
				combineErr = serviceErr
			} else {
				changes = combined.Items
				session.replaceProfile(combined.Profile)
			}
		}
	}
	responseData := game.CombineForgeResponse{
		Type: request.Type, ItemID: request.ItemID, FromItemID: request.FromItemID,
		MaterialIDs: request.MaterialIDs,
	}
	if combineErr != nil {
		responseData.ResultID = 1
		responseData.Reason = combineErr.Error()
		result.result = "combine_rejected"
		server.log(logEvent{
			Level: "warn", Event: "combine_rejected", ConnectionID: connectionID, AccountID: fmt.Sprint(request.UIN),
			MessageID: fmt.Sprintf("0x%04X", game.CombineForgeCommand), Result: "rejected", ErrorContext: combineErr.Error(),
		})
	} else {
		responseData.Reason = "合成成功"
		result.result = fmt.Sprintf("combine_product_%d", request.ItemID)
	}
	encoded, err := game.BuildLocalCombineForgeResponse(data, responseData)
	if err != nil {
		server.log(logEvent{Level: "warn", Event: "combine_response_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		return result
	}
	result.response = encoded
	if len(changes) != 0 {
		// ui_ReceiveComResult immediately re-queries the client's inventory and
		// redraws both the recipe count and material rows. The absolute inventory
		// notification must therefore be consumed before RESPONSE_COMBINE_FORGE;
		// sending it as a normal follow-up leaves the open compose panel stale.
		result.beforeResponse, err = game.BuildLocalPlayerItemAddNotification(data, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: uint32(time.Now().Unix()), SourceUIN: session.UIN, Items: changes,
		})
		if err != nil {
			result.beforeResponse = nil
			server.log(logEvent{Level: "warn", Event: "combine_inventory_refresh_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		} else {
			result.beforeResponseResult = fmt.Sprintf("combine_inventory_refresh_%d_items_before_result", len(changes))
		}
	}
	return result
}

func (server *Server) handleAvatarForge(session *connectionSession, connectionID string, data []byte, request game.ForgeRequest) loginServiceResult {
	result := loginServiceResult{handled: true, keepConnection: true}
	var forgeErr error
	if session.UIN == 0 || request.UIN != session.UIN {
		forgeErr = fmt.Errorf("forge UIN %d does not match session UIN %d", request.UIN, session.UIN)
	} else if request.ItemID == 0 || request.ItemID > math.MaxUint16 || request.MaterialID == 0 || request.MaterialID > math.MaxUint16 {
		forgeErr = fmt.Errorf("forge item/material IDs %d/%d are outside uint16 inventory range", request.ItemID, request.MaterialID)
	} else if server.forgeCatalog == nil {
		forgeErr = fmt.Errorf("avatar forge catalog is unavailable")
	}
	var applied craftcatalog.ForgePlan
	var current game.ItemInfo
	if forgeErr == nil {
		players, serviceErr := server.players()
		if serviceErr != nil {
			forgeErr = serviceErr
		} else {
			forgeResult, serviceErr := players.ForgeAvatar(
				context.Background(), session.UIN, server.forgeCatalog, craftcatalog.ForgeOperation(request.Type),
				uint16(request.ItemID), uint16(request.MaterialID), rand.Reader,
			)
			if serviceErr != nil {
				forgeErr = serviceErr
			}
			current = forgeResult.Item
			if serviceErr == nil {
				applied = forgeResult.Plan
				session.replaceProfile(forgeResult.Profile)
			}
		}
	}
	response := game.ForgeResponse{
		Type: request.Type, ItemID: request.ItemID, MaterialID: request.MaterialID,
		Effect: current.ItemEffect, Color: current.ItemColor,
	}
	if forgeErr != nil {
		response.ResultID = 1
		response.Reason = forgeErr.Error()
		server.log(logEvent{
			Level: "warn", Event: "avatar_forge_rejected", ConnectionID: connectionID,
			AccountID: fmt.Sprint(request.UIN), MessageID: fmt.Sprintf("0x%04X", game.ForgeCommand),
			Result: "rejected", ErrorContext: forgeErr.Error(),
		})
		result.result = "avatar_forge_rejected"
	} else {
		response.Effect = current.ItemEffect
		response.Color = current.ItemColor
		if applied.Succeeded {
			response.Reason = "锻造成功"
			result.result = fmt.Sprintf("avatar_forge_%d_success_effect_%d_color_%d", request.Type, current.ItemEffect, current.ItemColor)
		} else {
			response.ResultID = 1
			response.Reason = "锻造失败"
			result.result = fmt.Sprintf("avatar_forge_%d_chance_failed", request.Type)
		}
		material := game.NewPermanentItemInfo(uint16(request.MaterialID), currentQuantityOrZero(session.Profile.Inventory, uint16(request.MaterialID)))
		result.followUp, forgeErr = game.BuildLocalPlayerItemAddNotification(data, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: uint32(time.Now().Unix()), SourceUIN: session.UIN, Items: []game.ItemInfo{current, material},
		})
		if forgeErr == nil {
			result.followUpResult = "avatar_forge_inventory_refresh"
		} else {
			result.followUp = nil
			server.log(logEvent{Level: "warn", Event: "avatar_forge_inventory_refresh_failed", ConnectionID: connectionID, ErrorContext: forgeErr.Error()})
		}
	}
	encoded, err := game.BuildLocalForgeResponse(data, response)
	if err != nil {
		server.log(logEvent{Level: "warn", Event: "avatar_forge_response_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		return result
	}
	result.response = encoded
	return result
}

func currentQuantityOrZero(inventory []game.ItemInfo, itemID uint16) uint32 {
	for _, item := range inventory {
		if item.ItemID == itemID {
			return item.NumOfItem
		}
	}
	return 0
}
