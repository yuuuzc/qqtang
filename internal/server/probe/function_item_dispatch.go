package probe

import (
	"context"
	"fmt"
	"math"
	"time"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

// handleFunctionalItemMessage owns active operations from the original
// shop's “功能道具” category. Passive/equipped effects continue to travel in
// ITEM_INFO; operations that consume inventory are authoritative here.
func (server *Server) handleFunctionalItemMessage(session *connectionSession, connectionID string, data []byte) loginServiceResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil || inspection.Command != game.BreakEggRequestCommand {
		return loginServiceResult{keepConnection: true}
	}
	result := loginServiceResult{handled: true, keepConnection: true}
	request, operationErr := game.DecodeLocalBreakEggRequest(data)
	if operationErr == nil && (session.UIN == 0 || request.UIN != session.UIN) {
		operationErr = fmt.Errorf("break-egg UIN %d does not match session UIN %d", request.UIN, session.UIN)
	}
	if operationErr == nil && (request.EggID == 0 || request.EggID > math.MaxUint16 || request.HammerID == 0 || request.HammerID > math.MaxUint16) {
		operationErr = fmt.Errorf("break-egg item IDs egg=%d hammer=%d are outside inventory range", request.EggID, request.HammerID)
	}
	if operationErr == nil && server.breakEggCatalog == nil {
		operationErr = fmt.Errorf("break-egg reward catalog is unavailable")
	}
	if operationErr == nil && server.playerStore == nil {
		operationErr = fmt.Errorf("player store is unavailable")
	}

	var rewardID uint16
	var rewardQuantity uint32
	var rewardTier uint16
	var changed []game.ItemInfo
	if operationErr == nil {
		reward, tier, selectErr := server.breakEggCatalog.Select(uint16(request.EggID), uint16(request.HammerID), nil)
		if selectErr != nil {
			operationErr = selectErr
		} else {
			rewardID, rewardQuantity, rewardTier = reward.ItemID, reward.Quantity, tier
			players, serviceErr := server.players()
			if serviceErr != nil {
				operationErr = serviceErr
			} else {
				profile, itemChanges, exchangeErr := players.ExchangeInventoryItems(
					context.Background(), session.UIN,
					[]persistence.InventoryConsumption{
						{UIN: session.UIN, ItemID: uint16(request.EggID), Quantity: 1},
						{UIN: session.UIN, ItemID: uint16(request.HammerID), Quantity: 1},
					},
					[]game.ItemInfo{game.NewPermanentItemInfo(rewardID, rewardQuantity)},
				)
				if exchangeErr != nil {
					operationErr = exchangeErr
				} else {
					changed = itemChanges
					session.replaceProfile(profile)
					server.syncPrimarySessionProfile(session, session.UIN, session.Profile)
				}
			}
		}
	}

	response := game.BreakEggResponse{ResultID: 1, AttachInfo: "砸蛋失败"}
	if operationErr == nil {
		response.ResultID = 0
		response.ShowItemID = uint32(rewardID)
		response.AttachInfo = "砸蛋成功"
		result.result = fmt.Sprintf("break_egg_tier_%d_reward_%d_x%d", rewardTier, rewardID, rewardQuantity)
	} else {
		result.result = "break_egg_rejected"
		server.log(logEvent{
			Level: "warn", Event: "break_egg_rejected", ConnectionID: connectionID,
			AccountID: fmt.Sprint(session.UIN), MessageID: fmt.Sprintf("0x%04X", game.BreakEggRequestCommand),
			Result: "rejected", ErrorContext: operationErr.Error(),
		})
	}
	result.response, err = game.BuildLocalBreakEggResponse(data, response)
	if err != nil {
		result.response = nil
		server.log(logEvent{Level: "warn", Event: "break_egg_response_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		return result
	}
	if len(changed) != 0 {
		// The original result callback re-reads both the hammer count and egg
		// count immediately. Deliver absolute inventory rows first so its view
		// cannot lag behind the committed transaction.
		result.beforeResponse, err = game.BuildLocalPlayerItemAddNotification(data, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: uint32(time.Now().Unix()), SourceUIN: session.UIN, Items: changed,
		})
		if err != nil {
			result.beforeResponse = nil
			server.log(logEvent{Level: "warn", Event: "break_egg_inventory_refresh_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		} else {
			result.beforeResponseResult = fmt.Sprintf("break_egg_inventory_refresh_%d_items_before_result", len(changed))
		}
	}
	return result
}
