package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"qqtang/internal/game/equipment"
	"qqtang/internal/protocol/game"
)

// ApplyEquipmentSeed applies a versioned bootstrap exactly once. It grants
// account-owned items and creates role-specific loadout rows without replacing
// later shop changes on every restart.
func (store *PlayerStore) ApplyEquipmentSeed(ctx context.Context, seed equipment.Seed) (bool, error) {
	if store == nil || store.db == nil {
		return false, fmt.Errorf("player store is nil")
	}
	if seed.UIN == 0 || seed.Key == "" {
		return false, fmt.Errorf("equipment seed requires a non-zero UIN and key")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin equipment seed %q: %w", seed.Key, err)
	}
	defer tx.Rollback()
	var alreadyApplied int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM player_seed_history WHERE uin = ? AND seed_key = ?`, seed.UIN, seed.Key).Scan(&alreadyApplied)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read equipment seed history %q: %w", seed.Key, err)
	}
	var playerExists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM local_players WHERE uin = ?`, seed.UIN).Scan(&playerExists); err != nil {
		return false, fmt.Errorf("equipment seed %q player %d is unavailable: %w", seed.Key, seed.UIN, err)
	}
	for _, granted := range seed.Inventory {
		item := game.NewPermanentItemInfo(granted.ItemID, granted.Quantity)
		if _, err = tx.ExecContext(ctx, `INSERT INTO player_inventory(
			uin, item_id, quantity, item_status, item_role_id, item_effect, item_color, buy_time, available_period
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uin, item_id) DO UPDATE SET
			quantity = max(player_inventory.quantity, excluded.quantity),
			available_period = max(player_inventory.available_period, excluded.available_period)`,
			seed.UIN, item.ItemID, item.NumOfItem, item.ItemStatus, item.ItemRoleID,
			item.ItemEffect, item.ItemColor, item.BuyTime, item.AvailPeriod); err != nil {
			return false, fmt.Errorf("grant equipment seed %q item %d: %w", seed.Key, granted.ItemID, err)
		}
	}
	for _, assignment := range seed.Assignments {
		if _, err = tx.ExecContext(ctx, `DELETE FROM player_loadouts WHERE uin = ? AND item_id = ?`, seed.UIN, assignment.ItemID); err != nil {
			return false, fmt.Errorf("transfer equipment seed %q item %d: %w", seed.Key, assignment.ItemID, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO player_loadouts(uin, role_id, slot, item_id)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(uin, role_id, slot) DO UPDATE SET item_id = excluded.item_id`,
			seed.UIN, assignment.RoleID, string(assignment.Slot), assignment.ItemID); err != nil {
			return false, fmt.Errorf("apply equipment seed %q role %d slot %s: %w", seed.Key, assignment.RoleID, assignment.Slot, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO player_seed_history(uin, seed_key, applied_utc) VALUES(?, ?, ?)`,
		seed.UIN, seed.Key, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, fmt.Errorf("record equipment seed %q: %w", seed.Key, err)
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit equipment seed %q: %w", seed.Key, err)
	}
	return true, nil
}

func (store *PlayerStore) LoadEquipment(ctx context.Context, uin uint32) ([]equipment.Assignment, error) {
	if uin == 0 {
		return nil, fmt.Errorf("player UIN must be non-zero")
	}
	rows, err := store.db.QueryContext(ctx, `SELECT role_id, slot, item_id FROM player_loadouts WHERE uin = ?`, uin)
	if err != nil {
		return nil, fmt.Errorf("load equipment for UIN %d: %w", uin, err)
	}
	defer rows.Close()
	var assignments []equipment.Assignment
	for rows.Next() {
		var roleID, itemID int64
		var slot string
		if err := rows.Scan(&roleID, &slot, &itemID); err != nil {
			return nil, fmt.Errorf("scan equipment for UIN %d: %w", uin, err)
		}
		assignments = append(assignments, equipment.Assignment{RoleID: byte(roleID), Slot: equipment.Slot(slot), ItemID: uint16(itemID)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate equipment for UIN %d: %w", uin, err)
	}
	equipment.SortAssignments(assignments)
	return assignments, nil
}

// ApplyEquipmentChanges is called only by the shop's confirmed save request.
// Client-side preview calls never reach this method.
func (store *PlayerStore) ApplyEquipmentChanges(ctx context.Context, uin uint32, changes []equipment.Change) error {
	if uin == 0 {
		return fmt.Errorf("player UIN must be non-zero")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin equipment changes for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	if err = applyEquipmentChangesTx(ctx, tx, uin, changes); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit equipment changes for UIN %d: %w", uin, err)
	}
	return nil
}

// ReconcileLegacyEquipment imports the pre-loadout ITEM_INFO status/role
// representation exactly once. Callers have already resolved slot conflicts
// against the normalized assignments. Clearing the old flags in the same
// transaction prevents an unequipped legacy item from being resurrected by a
// later projection; 收藏柜 status 2 is deliberately untouched.
func (store *PlayerStore) ReconcileLegacyEquipment(ctx context.Context, uin uint32, changes []equipment.Change, legacyItemIDs []uint16) error {
	if uin == 0 {
		return fmt.Errorf("player UIN must be non-zero")
	}
	if len(legacyItemIDs) == 0 {
		return nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy equipment reconciliation for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	if err = applyEquipmentChangesTx(ctx, tx, uin, changes); err != nil {
		return err
	}
	for _, itemID := range legacyItemIDs {
		if itemID == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE player_inventory
			SET item_status = ?, item_role_id = 0
			WHERE uin = ? AND item_id = ? AND item_status = ?`,
			game.ItemStatusAvailable, uin, itemID, game.ItemStatusActive); err != nil {
			return fmt.Errorf("canonicalize legacy equipment item %d for UIN %d: %w", itemID, uin, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy equipment reconciliation for UIN %d: %w", uin, err)
	}
	return nil
}

// SaveItemStatusChanges atomically persists the legacy ITEM_INFO ownership
// flags and the normalized role loadout. This is required for 收藏柜 status 2:
// collecting an equipped item must never commit without also unequipping it.
func (store *PlayerStore) SaveItemStatusChanges(ctx context.Context, uin uint32, profile game.PlayerProfile, changes []equipment.Change) error {
	if uin == 0 {
		return fmt.Errorf("player UIN must be non-zero")
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin item-status changes for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	if err = saveProfileTx(ctx, tx, uin, profile); err != nil {
		return err
	}
	if err = applyEquipmentChangesTx(ctx, tx, uin, changes); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit item-status changes for UIN %d: %w", uin, err)
	}
	return nil
}

func applyEquipmentChangesTx(ctx context.Context, tx *sql.Tx, uin uint32, changes []equipment.Change) error {
	for _, change := range changes {
		if change.RoleID == 0 || change.Slot == "" || change.ItemID == 0 {
			return fmt.Errorf("invalid equipment change %+v", change)
		}
		if change.Equipped {
			var quantity uint32
			if err := tx.QueryRowContext(ctx, `SELECT quantity FROM player_inventory WHERE uin = ? AND item_id = ?`, uin, change.ItemID).Scan(&quantity); err != nil {
				return fmt.Errorf("equip item %d is not owned by UIN %d: %w", change.ItemID, uin, err)
			}
			if quantity == 0 {
				return fmt.Errorf("equip item %d has zero quantity", change.ItemID)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM player_loadouts WHERE uin = ? AND item_id = ?`, uin, change.ItemID); err != nil {
				return fmt.Errorf("transfer equipped item %d: %w", change.ItemID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO player_loadouts(uin, role_id, slot, item_id)
				VALUES(?, ?, ?, ?)
				ON CONFLICT(uin, role_id, slot) DO UPDATE SET item_id = excluded.item_id`,
				uin, change.RoleID, string(change.Slot), change.ItemID); err != nil {
				return fmt.Errorf("equip item %d: %w", change.ItemID, err)
			}
		} else if _, err := tx.ExecContext(ctx, `DELETE FROM player_loadouts WHERE uin = ? AND item_id = ?`,
			uin, change.ItemID); err != nil {
			return fmt.Errorf("unequip item %d: %w", change.ItemID, err)
		}
	}
	return nil
}

func insertInventoryItem(ctx context.Context, tx *sql.Tx, uin uint32, item game.ItemInfo, ignoreDuplicate bool) error {
	if item.NumOfItem > 0 {
		item.BuyTime = 0
		item.AvailPeriod = game.LocalPermanentAvailablePeriod
	}
	conflict := ""
	if ignoreDuplicate {
		conflict = " ON CONFLICT(uin, item_id) DO NOTHING"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO player_inventory(
		uin, item_id, quantity, item_status, item_role_id, item_effect, item_color, buy_time, available_period
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`+conflict,
		uin, item.ItemID, item.NumOfItem, item.ItemStatus, item.ItemRoleID, item.ItemEffect, item.ItemColor, item.BuyTime, item.AvailPeriod)
	return err
}
