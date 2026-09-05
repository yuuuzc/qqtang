package probe

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestAdventureSettlementInventoryItemsUseCommittedAbsoluteValues(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		{ItemID: 30069, NumOfItem: 6, ItemStatus: 1, AvailPeriod: game.LocalPermanentAvailablePeriod},
		{ItemID: 100, NumOfItem: 9, AvailPeriod: game.LocalPermanentAvailablePeriod},
		{ItemID: 20043, NumOfItem: 464, AvailPeriod: game.LocalPermanentAvailablePeriod},
	}
	items, err := adventureSettlementInventoryItems(profile, &adventureSettlementCommit{CollectedItems: map[uint32]uint32{
		30069: 1,
		100:   2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ItemID != 100 || items[0].NumOfItem != 9 || items[1].ItemID != 30069 || items[1].NumOfItem != 6 {
		t.Fatalf("settlement refresh items = %+v", items)
	}
	if items[1].ItemStatus != 1 {
		t.Fatalf("settlement refresh lost ITEM_INFO metadata: %+v", items[1])
	}
}
