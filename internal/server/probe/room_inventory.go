package probe

import (
	"context"
	"fmt"
	"math"
	"strings"

	"qqtang/internal/game/itemeffect"
	"qqtang/internal/game/itemuse"
	"qqtang/internal/protocol/game"
)

func (server *Server) updateSessionItemStatuses(session *connectionSession, request game.ItemStatusChangeRequest) error {
	if session == nil || session.UIN == 0 || request.UIN != session.UIN {
		return fmt.Errorf("item-status-change UIN %d does not match active session UIN %d", request.UIN, session.UIN)
	}
	players, err := server.players()
	if err != nil {
		return err
	}
	updated, err := players.UpdateItemStatuses(context.Background(), session.UIN, session.selectedRoleID(), session.Profile, request.Items)
	if err != nil {
		return err
	}
	if server.combineCatalog != nil {
		if err := players.ReconcileLearnedRecipes(context.Background(), session.UIN, server.combineCatalog); err != nil {
			return fmt.Errorf("reconcile learned recipes after item-status change: %w", err)
		}
	}
	session.replaceProfile(updated)
	server.syncPrimarySessionProfile(session, session.UIN, session.Profile)
	return nil
}

func describeItemStatusChanges(changes []game.ItemStatusChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, fmt.Sprintf("item_%d_status_%d_role_%d", change.ItemID, change.NewStatus, change.NewRoleID))
	}
	return strings.Join(parts, ",")
}

func inventoryItemsForStatusChanges(inventory []game.ItemInfo, changes []game.ItemStatusChange) []game.ItemInfo {
	items := make([]game.ItemInfo, 0, len(changes))
	for _, change := range changes {
		if change.ItemID == 0 || change.ItemID > math.MaxUint16 {
			continue
		}
		for _, item := range inventory {
			if item.ItemID == uint16(change.ItemID) {
				items = append(items, item)
				break
			}
		}
	}
	return items
}

// itemStatusChangesTouchCollection reports transitions into or out of the
// shop's 收藏柜. The distinction matters on the wire: the original client
// handles a successful RESPONSE_ITEM_STATUS_CHANGE by immediately rebuilding
// both the backpack and recycler from its native ITEM_INFO cache.
func itemStatusChangesTouchCollection(inventory []game.ItemInfo, changes []game.ItemStatusChange) bool {
	for _, change := range changes {
		if change.NewStatus == game.ItemStatusCollected {
			return true
		}
		if change.ItemID == 0 || change.ItemID > math.MaxUint16 {
			continue
		}
		for _, item := range inventory {
			if item.ItemID == uint16(change.ItemID) && item.ItemStatus == game.ItemStatusCollected {
				return true
			}
		}
	}
	return false
}

func (server *Server) preparedMatchItems(profile game.PlayerProfile, roleID byte, scope itemeffect.Scope) ([]game.GameItemType, error) {
	items := make([]game.GameItemType, 0, game.PlayerGameInfoMaxNewItemTypes)
	for _, item := range profile.ToLocalLoginConfig().Items {
		if !item.Active() || item.ItemStatus == 0 || (item.ItemRoleID != 0 && item.ItemRoleID != roleID) {
			continue
		}
		// ITEM_INFO status is shared by two client systems: role cosmetics and
		// prepared match consumables. Only the latter belongs in
		// PLAYER_GAME_INFO.NewItems. Projected loadouts can contain more than ten
		// equipped cosmetics and must never consume the gameplay item vector.
		if server.equipmentCatalog != nil {
			if _, cosmetic := server.equipmentCatalog.Lookup(item.ItemID); cosmetic {
				continue
			}
		}
		if kind, known := server.itemKindsByID[item.ItemID]; known && kind != "inventory-consumable" {
			continue
		}
		if !itemeffect.AllowsPrepared(uint32(item.ItemID), scope) {
			continue
		}
		if len(items) == game.PlayerGameInfoMaxNewItemTypes {
			return nil, fmt.Errorf("prepared item types exceed PLAYER_GAME_INFO limit %d", game.PlayerGameInfoMaxNewItemTypes)
		}
		quantity := item.NumOfItem
		if quantity > math.MaxInt16 {
			quantity = math.MaxInt16
		}
		items = append(items, game.GameItemType{ItemID: uint32(item.ItemID), Quantity: int16(quantity)})
	}
	return items, nil
}

func (server *Server) preparedAdventureItems(profile game.PlayerProfile, roleID byte) ([]game.GameItemType, error) {
	return server.preparedMatchItems(profile, roleID, itemeffect.ScopeAdventure)
}

func (server *Server) consumeSessionPreparedItem(session *connectionSession, use game.PreparedUsePropEvent) (game.ItemInfo, *connectionSession, error) {
	if session == nil || session.UIN == 0 || session.CurrentGameID == 0 {
		return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop requires an active game session")
	}
	if _, err := server.requireMatchParticipantActor(session, use.PlayerID, "prepared-use-prop"); err != nil {
		return game.ItemInfo{}, nil, err
	}
	actor := session
	if use.PlayerID != session.Profile.PlayerID {
		actor = server.liveRoomSessionForPlayer(session.RoomID, use.PlayerID)
		if actor == nil {
			return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop actor %d has no live room session", use.PlayerID)
		}
	}
	if use.ItemID == 0 || use.ItemID > math.MaxUint16 {
		return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop item ID %d is outside inventory range", use.ItemID)
	}
	plan, err := server.resolveMatchItemUse(session, itemuse.PathPreparedProp, use.ItemID)
	if err != nil {
		return game.ItemInfo{}, nil, err
	}
	if plan.Consumption != itemuse.ConsumeOnAccepted {
		return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop item %d resolved unsafe consumption %q", use.ItemID, plan.Consumption)
	}
	if server.equipmentCatalog != nil {
		if _, cosmetic := server.equipmentCatalog.Lookup(uint16(use.ItemID)); cosmetic {
			return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop item %d is cosmetic equipment", use.ItemID)
		}
	}
	if server.playerStore != nil {
		players, err := server.players()
		if err != nil {
			return game.ItemInfo{}, nil, err
		}
		actorUIN := actor.liveUIN.Load()
		if actor == session {
			actorUIN = session.UIN
		}
		updated, remaining, err := players.ConsumePreparedItem(context.Background(), actorUIN, actor.routingRoleID(), game.PlayerProfile{}, uint16(use.ItemID))
		if err != nil {
			return game.ItemInfo{}, nil, err
		}
		if actor == session {
			session.replaceProfile(updated)
		} else {
			cloned := clonePlayerProfile(updated)
			actor.pendingProfile.Store(&cloned)
			server.updateAuthenticatedSessionProfile(actor.remoteAddress, actorUIN, updated)
		}
		return remaining, actor, nil
	}
	// Persistence-free room unit tests retain a narrow in-memory path. Every
	// production configuration with accounts uses PlayerService above.
	if actor != session {
		return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop proxy requires the persistent player store")
	}
	for _, item := range actor.Profile.Inventory {
		if item.ItemID == uint16(use.ItemID) && item.Active() && item.ItemStatus != 0 && (item.ItemRoleID == 0 || item.ItemRoleID == actor.selectedRoleID()) {
			if err := actor.Profile.SetPermanentInventoryItem(item.ItemID, item.NumOfItem-1); err != nil {
				return game.ItemInfo{}, nil, err
			}
			item.NumOfItem--
			return item, actor, nil
		}
	}
	return game.ItemInfo{}, nil, fmt.Errorf("prepared-use-prop item %d is not in the actor inventory", use.ItemID)
}
