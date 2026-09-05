package winlaunch

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestParseRoomInventoryItems(t *testing.T) {
	first := game.ItemInfo{ItemID: 99, NumOfItem: 1}.MemoryBinary()
	second := game.ItemInfo{ItemID: 1234, NumOfItem: 5, ItemStatus: 6, BuyTime: 7, AvailPeriod: 8}.MemoryBinary()
	data := append(first[:], second[:]...)

	items := parseRoomInventoryItems(data, 2)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ItemID != 99 || items[0].NumOfItem != 1 {
		t.Fatalf("first item = %+v, want ID 99 quantity 1", items[0])
	}
	if items[1].ItemID != 1234 || items[1].NumOfItem != 5 || items[1].ItemStatus != 6 || items[1].BuyTime != 7 || items[1].AvailPeriod != 8 {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestParseRoomInventoryItemsStopsAtAvailableData(t *testing.T) {
	items := parseRoomInventoryItems(make([]byte, qqtSectionRoomInventoryItemSize), 2)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
}
