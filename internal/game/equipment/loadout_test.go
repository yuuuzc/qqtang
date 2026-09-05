package equipment

import (
	"os"
	"path/filepath"
	"testing"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
)

func TestProjectInventoryUsesLastCurrentRoleDefensively(t *testing.T) {
	catalog, err := NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, RegistryCategory: "cap", Name: "hat", Categories: []string{"材料", "cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := []game.ItemInfo{{ItemID: 22, NumOfItem: 1, AvailPeriod: game.LocalPermanentAvailablePeriod}}
	projected := catalog.ProjectInventory(inventory, []Assignment{
		{RoleID: 1, Slot: SlotHeadFront, ItemID: 22},
		{RoleID: 7, Slot: SlotHeadFront, ItemID: 22},
	})
	if projected[0].ItemStatus != 1 || projected[0].ItemRoleID != 7 || projected[0].NumOfItem != 1 {
		t.Fatalf("single-role projection = %+v", projected[0])
	}
	if inventory[0].ItemStatus != 0 {
		t.Fatalf("projection mutated ownership input: %+v", inventory[0])
	}
}

func TestItemIDsForRoleAreDeterministicAndRoleScoped(t *testing.T) {
	catalog, err := NewCatalog([]itemcatalog.Entry{
		{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}},
		{ID: 12087, Index: 92, Name: "card", Categories: []string{"namecard"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.ItemIDsForRole([]Assignment{
		{RoleID: 7, Slot: SlotNamecard, ItemID: 12087},
		{RoleID: 7, Slot: SlotHeadFront, ItemID: 22},
	}, 7)
	if len(got) != 2 || got[0] != 22 || got[1] != 12087 {
		t.Fatalf("role 7 external items = %v", got)
	}
}

func TestProjectInventoryForRoomUsesSingleCurrentRole(t *testing.T) {
	catalog, err := NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := []game.ItemInfo{game.NewPermanentItemInfo(22, 1)}
	assignments := []Assignment{
		{RoleID: 7, Slot: SlotHeadFront, ItemID: 22},
	}
	projected := catalog.ProjectInventoryForRoom(inventory, assignments)
	if len(projected) != 1 || projected[0].ItemStatus != 1 || projected[0].ItemRoleID != 7 {
		t.Fatalf("room role projection = %+v", projected)
	}
}

func TestPlatformRoleEffectsShareOneNativeSlot(t *testing.T) {
	catalog, err := NewCatalog([]itemcatalog.Entry{
		{ID: 199, Index: 18, RegistryCategory: "platform", Name: "lightning"},
		{ID: 467, Index: 19, RegistryCategory: "platform", Name: "scimitar"},
		{ID: 468, Index: 20, RegistryCategory: "platform", Name: "kick"},
		{ID: 294, Index: 1, RegistryCategory: "platform", Name: "bugle"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, itemID := range []uint16{199, 467, 468} {
		item, ok := catalog.Lookup(itemID)
		if !ok || item.Slot != SlotPlatformEffect {
			t.Fatalf("platform effect %d = %+v, found %v", itemID, item, ok)
		}
	}
	if _, ok := catalog.Lookup(294); ok {
		t.Fatal("active bugle operation was misclassified as role equipment")
	}
}

func TestLoadSeedsRejectsCategorySlotMismatch(t *testing.T) {
	catalog, err := NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "loadouts.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"seeds":[{"key":"v1","uin":1,"inventory":[{"item_id":22,"quantity":1,"name":"hat"}],"assignments":[{"role_id":7,"slot":"foot","item_id":22}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeeds(path, catalog); err == nil {
		t.Fatal("LoadSeeds accepted a cap in the foot slot")
	}
}
