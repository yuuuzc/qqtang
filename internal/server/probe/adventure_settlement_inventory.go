package probe

import (
	"fmt"
	"net"
	"sort"
	"time"

	"qqtang/internal/protocol/game"
)

const adventureSettlementInventoryRefreshDelay = 100 * time.Millisecond

// adventureSettlementInventoryItems returns the authoritative, absolute
// ITEM_INFO records changed by the just-committed adventure settlement. The
// settlement map identifies the changed records; the profile supplies their
// final quantities and preserves status/equipment metadata.
func adventureSettlementInventoryItems(profile game.PlayerProfile, settlement *adventureSettlementCommit) ([]game.ItemInfo, error) {
	if settlement == nil {
		return nil, nil
	}
	return settlementInventoryItems(profile, settlement.CollectedItems)
}

func settlementInventoryItems(profile game.PlayerProfile, collectedItems map[uint32]uint32) ([]game.ItemInfo, error) {
	if len(collectedItems) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(collectedItems))
	for itemID, quantity := range collectedItems {
		if quantity > 0 {
			ids = append(ids, int(itemID))
		}
	}
	sort.Ints(ids)
	items := make([]game.ItemInfo, 0, len(ids))
	for _, rawID := range ids {
		itemID := uint16(rawID)
		found := false
		for _, item := range profile.Inventory {
			if item.ItemID != itemID {
				continue
			}
			if !item.Active() {
				return nil, fmt.Errorf("settlement inventory item %d is inactive after commit", itemID)
			}
			items = append(items, item)
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("settlement inventory item %d is absent after commit", itemID)
		}
	}
	return items, nil
}

func competitiveSettlementInventoryItems(profile game.PlayerProfile, settlement *competitiveSettlementCommit) ([]game.ItemInfo, error) {
	if settlement == nil {
		return nil, nil
	}
	return settlementInventoryItems(profile, settlement.CollectedItems)
}

func (server *Server) writeAdventureSettlementInventoryRefresh(
	connection net.Conn,
	session *connectionSession,
	connectionID, local, remote string,
	requestPacket []byte,
	settlement *adventureSettlementCommit,
) bool {
	items, err := adventureSettlementInventoryItems(session.Profile, settlement)
	return server.writeSettlementInventoryRefresh(connection, session, connectionID, local, remote, requestPacket, items, err, "adventure")
}

func (server *Server) writeCompetitiveSettlementInventoryRefresh(
	connection net.Conn,
	session *connectionSession,
	connectionID, local, remote string,
	requestPacket []byte,
	settlement *competitiveSettlementCommit,
) bool {
	items, err := competitiveSettlementInventoryItems(session.Profile, settlement)
	return server.writeSettlementInventoryRefresh(connection, session, connectionID, local, remote, requestPacket, items, err, "competitive")
}

func (server *Server) writeSettlementInventoryRefresh(
	connection net.Conn,
	session *connectionSession,
	connectionID, local, remote string,
	requestPacket []byte,
	items []game.ItemInfo,
	err error,
	category string,
) bool {
	if err != nil {
		server.log(logEvent{Level: "error", Event: category + "_settlement_inventory_refresh_failed", ConnectionID: connectionID, ErrorContext: err.Error()})
		return false
	}
	if len(items) == 0 {
		return true
	}
	time.Sleep(adventureSettlementInventoryRefreshDelay)
	batchCount := (len(items) + game.MaxPlayerItemAddItems - 1) / game.MaxPlayerItemAddItems
	now := uint32(time.Now().Unix())
	for batchIndex, start := 0, 0; start < len(items); batchIndex, start = batchIndex+1, start+game.MaxPlayerItemAddItems {
		end := start + game.MaxPlayerItemAddItems
		if end > len(items) {
			end = len(items)
		}
		packet, buildErr := game.BuildLocalPlayerItemAddNotification(requestPacket, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: now, SourceUIN: session.UIN,
			Items: append([]game.ItemInfo(nil), items[start:end]...),
		})
		if buildErr != nil {
			server.log(logEvent{Level: "error", Event: category + "_settlement_inventory_refresh_failed", ConnectionID: connectionID, ErrorContext: buildErr.Error()})
			return false
		}
		result := fmt.Sprintf("qqt_%s_settlement_inventory_refresh_batch_%d_of_%d_items_%d", category, batchIndex+1, batchCount, end-start)
		if !server.writeTCP(connection, connectionID, local, remote, packet, result) {
			return false
		}
		if end < len(items) {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return true
}
