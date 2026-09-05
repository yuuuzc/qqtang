package game

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestItemInfoNetworkAndMemoryVectors(t *testing.T) {
	item := ItemInfo{
		ItemID: 99, NumOfItem: 2,
		ItemStatus: 3, ItemRoleID: 4, ItemEffect: 5, ItemColor: 6,
		BuyTime: 0x11223344, AvailPeriod: 0x55667788,
	}
	networkWant, _ := hex.DecodeString("006300000002030405061122334455667788")
	if got := item.AppendNetworkBinary(nil); !bytes.Equal(got, networkWant) {
		t.Fatalf("network ITEM_INFO = %x, want %x", got, networkWant)
	}
	memoryWant, _ := hex.DecodeString("630002000000030405064433221188776655")
	memory := item.MemoryBinary()
	if !bytes.Equal(memory[:], memoryWant) {
		t.Fatalf("memory ITEM_INFO = %x, want %x", memory, memoryWant)
	}
	for name, test := range map[string]struct {
		data  []byte
		parse func([]byte) (ItemInfo, error)
	}{
		"network": {networkWant, ParseItemInfoNetwork},
		"memory":  {memoryWant, ParseItemInfoMemory},
	} {
		got, err := test.parse(test.data)
		if err != nil {
			t.Fatalf("%s parse: %v", name, err)
		}
		if got != item {
			t.Fatalf("%s round trip = %+v, want %+v", name, got, item)
		}
	}
}

func TestParseItemInfoRejectsShortData(t *testing.T) {
	if _, err := ParseItemInfoMemory(make([]byte, ItemInfoBinarySize-1)); err == nil {
		t.Fatal("expected short ITEM_INFO memory data to fail")
	}
	if _, err := ParseItemInfoNetwork(make([]byte, ItemInfoBinarySize-1)); err == nil {
		t.Fatal("expected short ITEM_INFO network data to fail")
	}
}

func TestPermanentItemInfoIsActive(t *testing.T) {
	item := NewPermanentItemInfo(SinglePlayerAdventureCardItemID, 1)
	if !item.Active() || item.AvailPeriod != LocalPermanentAvailablePeriod {
		t.Fatalf("permanent ITEM_INFO = %+v", item)
	}
}
