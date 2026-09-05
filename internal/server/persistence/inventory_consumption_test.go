package persistence

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestConsumeInventoryItemsIsAtomicAcrossPlayers(t *testing.T) {
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	profiles := []struct {
		uin      uint32
		playerID uint16
	}{
		{uin: 1_000_001, playerID: 1},
		{uin: 1_000_002, playerID: 2},
	}
	for _, entry := range profiles {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = entry.playerID
		profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(460, 1)}
		if _, err = store.LoadOrCreate(ctx, entry.uin, profile); err != nil {
			t.Fatal(err)
		}
	}

	_, err = store.ConsumeInventoryItems(ctx, []InventoryConsumption{
		{UIN: profiles[0].uin, ItemID: 460, Quantity: 1},
		{UIN: profiles[1].uin, ItemID: 460, Quantity: 2},
	})
	if !errors.Is(err, ErrInventoryItemMissing) {
		t.Fatalf("partial Boss summon debit error = %v", err)
	}
	for _, entry := range profiles {
		item, found, loadErr := store.InventoryItem(ctx, entry.uin, 460)
		if loadErr != nil || !found || item.NumOfItem != 1 {
			t.Fatalf("UIN %d item after rejected debit = found:%t item:%+v err:%v", entry.uin, found, item, loadErr)
		}
	}

	changed, err := store.ConsumeInventoryItems(ctx, []InventoryConsumption{
		{UIN: profiles[0].uin, ItemID: 460, Quantity: 1},
		{UIN: profiles[1].uin, ItemID: 460, Quantity: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed[profiles[0].uin][0].NumOfItem != 0 || changed[profiles[1].uin][0].NumOfItem != 0 {
		t.Fatalf("committed Boss summon debit = %+v", changed)
	}
}
