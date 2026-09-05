package persistence

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestExchangeInventoryItemsIsAtomicAndReturnsAbsoluteRows(t *testing.T) {
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		game.NewPermanentItemInfo(9001, 2),
		game.NewPermanentItemInfo(9011, 1),
		game.NewPermanentItemInfo(294, 3),
	}
	if _, err = store.LoadOrCreate(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}

	_, err = store.ExchangeInventoryItems(ctx, uin, []InventoryConsumption{
		{UIN: uin, ItemID: 9001, Quantity: 1},
		{UIN: uin, ItemID: 9011, Quantity: 2},
	}, []game.ItemInfo{game.NewPermanentItemInfo(294, 1)})
	if !errors.Is(err, ErrInventoryItemMissing) {
		t.Fatalf("rejected exchange error = %v", err)
	}
	for itemID, want := range map[uint16]uint32{9001: 2, 9011: 1, 294: 3} {
		item, found, loadErr := store.InventoryItem(ctx, uin, itemID)
		if loadErr != nil || !found || item.NumOfItem != want {
			t.Fatalf("item %d after rollback = found:%t item:%+v err:%v", itemID, found, item, loadErr)
		}
	}

	changed, err := store.ExchangeInventoryItems(ctx, uin, []InventoryConsumption{
		{UIN: uin, ItemID: 9001, Quantity: 1},
		{UIN: uin, ItemID: 9011, Quantity: 1},
	}, []game.ItemInfo{game.NewPermanentItemInfo(294, 2)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint16]uint32{294: 5, 9001: 1, 9011: 0}
	if len(changed) != len(want) {
		t.Fatalf("changed rows = %+v", changed)
	}
	for _, item := range changed {
		if item.NumOfItem != want[item.ItemID] {
			t.Fatalf("absolute row %+v, want quantity %d", item, want[item.ItemID])
		}
	}
	if _, found, loadErr := store.InventoryItem(ctx, uin, 9011); loadErr != nil || found {
		t.Fatalf("exhausted hammer persisted = found:%t err:%v", found, loadErr)
	}
}
