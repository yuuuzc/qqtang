package probe

import (
	"context"
	"fmt"

	"qqtang/internal/game/equipment"
	"qqtang/internal/protocol/game"
)

// projectProfileEquipment rebuilds the old client's ITEM_INFO view from the
// normalized account inventory plus per-role loadouts. SQLite deliberately
// does not persist this lossy status/role projection, so every independently
// loaded lobby or room profile must pass through here before serialization.
func (server *Server) projectProfileEquipment(ctx context.Context, uin uint32, profile game.PlayerProfile) (game.PlayerProfile, []equipment.Assignment, error) {
	if server.playerStore == nil || server.equipmentCatalog == nil {
		return profile, nil, nil
	}
	assignments, err := server.playerStore.LoadEquipment(ctx, uin)
	if err != nil {
		return game.PlayerProfile{}, nil, fmt.Errorf("load equipment for UIN %d: %w", uin, err)
	}
	profile.Inventory = server.equipmentCatalog.ProjectInventory(profile.Inventory, assignments)
	return profile, assignments, nil
}
