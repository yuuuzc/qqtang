package equipment

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
)

// Slot is the durable equipment position. Values mirror the client's
// uiConst.equipMap where it has a physical avatar slot, while profile and
// gameplay decorations use explicit logical slots.
type Slot string

const (
	SlotHeadFront      Slot = "head_front"
	SlotHeadBack       Slot = "head_back"
	SlotHeadTop        Slot = "head_top"
	SlotHair           Slot = "hair"
	SlotFace           Slot = "face"
	SlotMouth          Slot = "mouth"
	SlotEar            Slot = "ear"
	SlotCloth          Slot = "cloth"
	SlotClothAdornment Slot = "cloth_adornment"
	SlotFrontPack      Slot = "front_pack"
	SlotBackPack       Slot = "back_pack"
	SlotLeg            Slot = "leg"
	SlotFoot           Slot = "foot"
	SlotBubble         Slot = "bubble"
	SlotPhantom        Slot = "phantom"
	SlotFootprint      Slot = "footprint"
	SlotBackground     Slot = "background"
	SlotFrame          Slot = "frame"
	SlotEntrance       Slot = "entrance"
	SlotNamecard       Slot = "namecard"
	SlotNamecardBound  Slot = "namecard_bound"
	// SlotPlatformEffect is the shop's single non-preview `platform` selection.
	// uiShop.py persists exactly one selected platform item per role.  The
	// battlefield effect itself remains map/action scoped and is never persisted
	// here as a second state flag.
	SlotPlatformEffect Slot = "platform_effect"
)

var categorySlots = map[string]Slot{
	"cap": SlotHeadFront, "fhadorn": SlotHeadFront,
	"bhadorn": SlotHeadBack, "thadorn": SlotHeadTop,
	"hair": SlotHair, "mask": SlotFace, "eye": SlotFace,
	"mouth": SlotMouth, "ear": SlotEar, "cloth": SlotCloth,
	"cladorn": SlotClothAdornment, "fpack": SlotFrontPack,
	"npack": SlotBackPack, "leg": SlotLeg, "foot": SlotFoot,
	"bomb": SlotBubble, "huanying": SlotPhantom,
	"footprint": SlotFootprint, "bg": SlotBackground,
	"frame": SlotFrame, "enter": SlotEntrance,
	"namecard": SlotNamecard, "namecardbound": SlotNamecardBound,
}

type Item struct {
	ID       uint16 `json:"id"`
	Index    uint32 `json:"index"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Slot     Slot   `json:"slot"`
}

type Catalog struct {
	items map[uint16]Item
}

func NewCatalog(entries []itemcatalog.Entry) (*Catalog, error) {
	catalog := &Catalog{items: make(map[uint16]Item)}
	for _, entry := range entries {
		if entry.ID == 0 || entry.ID > 0xffff {
			continue
		}
		category, slot, ok := equipmentCategory(entry.RegistryCategory, entry.Categories)
		if isRolePlatformEffect(entry.ID) {
			category, slot, ok = "platform", SlotPlatformEffect, true
		}
		if !ok {
			continue
		}
		item := Item{ID: uint16(entry.ID), Index: entry.Index, Name: entry.Name, Category: category, Slot: slot}
		if existing, duplicate := catalog.items[item.ID]; duplicate && existing != item {
			return nil, fmt.Errorf("item %d has conflicting equipment definitions %+v and %+v", item.ID, existing, item)
		}
		catalog.items[item.ID] = item
	}
	return catalog, nil
}

func isRolePlatformEffect(itemID uint32) bool {
	switch itemID {
	case 199, 467, 468:
		return true
	default:
		return false
	}
}

func equipmentCategory(registryCategory string, categories []string) (string, Slot, bool) {
	if slot, ok := categorySlots[registryCategory]; ok {
		return registryCategory, slot, true
	}
	for _, category := range categories {
		if slot, ok := categorySlots[category]; ok {
			return category, slot, true
		}
	}
	return "", "", false
}

func (catalog *Catalog) Lookup(itemID uint16) (Item, bool) {
	if catalog == nil {
		return Item{}, false
	}
	item, ok := catalog.items[itemID]
	return item, ok
}

type Assignment struct {
	RoleID byte   `json:"role_id"`
	Slot   Slot   `json:"slot"`
	ItemID uint16 `json:"item_id"`
}

type Change struct {
	RoleID   byte
	Slot     Slot
	ItemID   uint16
	Equipped bool
}

type SeedSet struct {
	SchemaVersion int    `json:"schema_version"`
	Seeds         []Seed `json:"seeds"`
}

type Seed struct {
	Key         string       `json:"key"`
	UIN         uint32       `json:"uin"`
	Inventory   []SeedItem   `json:"inventory"`
	Assignments []Assignment `json:"assignments"`
}

type SeedItem struct {
	ItemID   uint16 `json:"item_id"`
	Quantity uint32 `json:"quantity"`
	Name     string `json:"name,omitempty"`
}

func LoadSeeds(path string, catalog *Catalog) (SeedSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SeedSet{}, fmt.Errorf("read equipment seeds: %w", err)
	}
	var seeds SeedSet
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&seeds); err != nil {
		return SeedSet{}, fmt.Errorf("decode equipment seeds: %w", err)
	}
	if seeds.SchemaVersion != 1 {
		return SeedSet{}, fmt.Errorf("equipment seed schema_version %d, want 1", seeds.SchemaVersion)
	}
	seenKeys := make(map[string]struct{})
	for seedIndex := range seeds.Seeds {
		seed := &seeds.Seeds[seedIndex]
		seed.Key = strings.TrimSpace(seed.Key)
		if seed.Key == "" || seed.UIN == 0 {
			return SeedSet{}, fmt.Errorf("equipment seed %d requires a key and non-zero UIN", seedIndex)
		}
		if _, duplicate := seenKeys[seed.Key]; duplicate {
			return SeedSet{}, fmt.Errorf("duplicate equipment seed key %q", seed.Key)
		}
		seenKeys[seed.Key] = struct{}{}
		owned := make(map[uint16]struct{}, len(seed.Inventory))
		for itemIndex := range seed.Inventory {
			item := &seed.Inventory[itemIndex]
			if item.ItemID == 0 || item.Quantity == 0 {
				return SeedSet{}, fmt.Errorf("equipment seed %q inventory[%d] requires non-zero item and quantity", seed.Key, itemIndex)
			}
			definition, ok := catalog.Lookup(item.ItemID)
			if !ok {
				return SeedSet{}, fmt.Errorf("equipment seed %q item %d is not a client equipment item", seed.Key, item.ItemID)
			}
			if item.Name != "" && item.Name != definition.Name {
				return SeedSet{}, fmt.Errorf("equipment seed %q item %d name %q, client catalog says %q", seed.Key, item.ItemID, item.Name, definition.Name)
			}
			item.Name = definition.Name
			if _, duplicate := owned[item.ItemID]; duplicate {
				return SeedSet{}, fmt.Errorf("equipment seed %q repeats inventory item %d", seed.Key, item.ItemID)
			}
			owned[item.ItemID] = struct{}{}
		}
		positions := make(map[string]struct{}, len(seed.Assignments))
		assignedItems := make(map[uint16]struct{}, len(seed.Assignments))
		for assignmentIndex, assignment := range seed.Assignments {
			definition, ok := catalog.Lookup(assignment.ItemID)
			if assignment.RoleID == 0 || !ok || definition.Slot != assignment.Slot {
				return SeedSet{}, fmt.Errorf("equipment seed %q assignment[%d] is invalid for item %d", seed.Key, assignmentIndex, assignment.ItemID)
			}
			if _, ok := owned[assignment.ItemID]; !ok {
				return SeedSet{}, fmt.Errorf("equipment seed %q assignment item %d is not owned by the seed", seed.Key, assignment.ItemID)
			}
			position := fmt.Sprintf("%d/%s", assignment.RoleID, assignment.Slot)
			if _, duplicate := positions[position]; duplicate {
				return SeedSet{}, fmt.Errorf("equipment seed %q repeats role slot %s", seed.Key, position)
			}
			if _, duplicate := assignedItems[assignment.ItemID]; duplicate {
				return SeedSet{}, fmt.Errorf("equipment seed %q assigns item %d to several roles", seed.Key, assignment.ItemID)
			}
			positions[position] = struct{}{}
			assignedItems[assignment.ItemID] = struct{}{}
		}
	}
	return seeds, nil
}

// ProjectInventory converts normalized role loadouts into the legacy ITEM_INFO
// status/role projection consumed by GetRoleItem. Ownership and quantity are
// unchanged. Each inventory item has at most one active role assignment; a
// later shop save transfers it to the new role. Role zero represents an
// unequipped or client-defined role-unrestricted item, never several durable
// role references.
func (catalog *Catalog) ProjectInventory(inventory []game.ItemInfo, assignments []Assignment) []game.ItemInfo {
	projected := append([]game.ItemInfo(nil), inventory...)
	byID := make(map[uint16]byte)
	for _, assignment := range assignments {
		if assignment.RoleID != 0 {
			// Last change wins defensively if a caller supplies corrupt legacy
			// assignments. SQLite schema 7 prevents this ambiguity at rest.
			byID[assignment.ItemID] = assignment.RoleID
		}
	}
	for index := range projected {
		if _, cosmetic := catalog.Lookup(projected[index].ItemID); !cosmetic {
			continue
		}
		roleID := byID[projected[index].ItemID]
		if roleID == 0 {
			// Status 2 is the independent 收藏柜 flag. Do not erase it merely
			// because the item has no loadout assignment. Status 1, however, is
			// the lossy legacy equipment projection and must be cleared when the
			// normalized loadout no longer contains the item.
			if projected[index].ItemStatus == game.ItemStatusActive {
				projected[index].ItemStatus = game.ItemStatusAvailable
			}
			projected[index].ItemRoleID = 0
			continue
		}
		projected[index].ItemStatus = game.ItemStatusActive
		projected[index].ItemRoleID = roleID
	}
	return projected
}

// ProjectInventoryForRoom preserves the dedicated call site for the waiting
// room, where Client+0x458240 compares ItemRoleID with the displayed role.
// Schema 7 guarantees one active role per inventory item, so room and profile
// projections now share the same ITEM_INFO representation.
func (catalog *Catalog) ProjectInventoryForRoom(inventory []game.ItemInfo, assignments []Assignment) []game.ItemInfo {
	return catalog.ProjectInventory(inventory, assignments)
}

// ItemIDsForRole returns the durable loadout projection used by the lobby's
// PLAYER_INFO_OLD.ExtItemIDs. The client currently renders name cards from
// this array and safely ignores the other equipped categories, while room
// snapshots consume the full ITEM_INFO projection above.
func (catalog *Catalog) ItemIDsForRole(assignments []Assignment, roleID byte) []uint32 {
	if catalog == nil || roleID == 0 {
		return nil
	}
	ordered := append([]Assignment(nil), assignments...)
	SortAssignments(ordered)
	seen := make(map[uint16]struct{}, len(ordered))
	itemIDs := make([]uint32, 0, len(ordered))
	for _, assignment := range ordered {
		if assignment.RoleID != roleID {
			continue
		}
		if _, known := catalog.Lookup(assignment.ItemID); !known {
			continue
		}
		if _, duplicate := seen[assignment.ItemID]; duplicate {
			continue
		}
		seen[assignment.ItemID] = struct{}{}
		itemIDs = append(itemIDs, uint32(assignment.ItemID))
	}
	return itemIDs
}

func SortAssignments(assignments []Assignment) {
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].RoleID != assignments[j].RoleID {
			return assignments[i].RoleID < assignments[j].RoleID
		}
		if assignments[i].Slot != assignments[j].Slot {
			return assignments[i].Slot < assignments[j].Slot
		}
		return assignments[i].ItemID < assignments[j].ItemID
	})
}
