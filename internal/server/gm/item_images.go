package gm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"qqtang/internal/game/itemcatalog"
)

type itemImageRef struct {
	path          string
	archivePath   string
	source        string
	tightFrame    bool
	availableRole byte
}

type itemImageAlias struct {
	targetItemID uint32
	ref          itemImageRef
}

type itemImageAliasEntry struct {
	ItemID          uint32 `json:"item_id"`
	CanonicalItemID uint32 `json:"canonical_item_id,omitempty"`
	RelativePath    string `json:"relative_path,omitempty"`
	TightFrame      bool   `json:"tight_frame,omitempty"`
	Basis           string `json:"basis"`
}

type itemImageAliasConfig struct {
	SchemaVersion int                   `json:"schema_version"`
	Aliases       []itemImageAliasEntry `json:"aliases"`
}

func WithItemImageAliases(configPath string) Option {
	return func(server *Server) error {
		if strings.TrimSpace(configPath) == "" {
			return nil
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read GM item image aliases: %w", err)
		}
		var config itemImageAliasConfig
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return fmt.Errorf("decode GM item image aliases: %w", err)
		}
		if config.SchemaVersion != 1 {
			return fmt.Errorf("GM item image alias schema %d, want 1", config.SchemaVersion)
		}
		aliases := make(map[uint32]itemImageAlias, len(config.Aliases))
		root, err := filepath.Abs(server.clientRoot)
		if err != nil {
			return err
		}
		for index, entry := range config.Aliases {
			if entry.ItemID == 0 || strings.TrimSpace(entry.Basis) == "" ||
				(entry.CanonicalItemID == 0) == (strings.TrimSpace(entry.RelativePath) == "") {
				return fmt.Errorf("GM item image alias %d is incomplete", index)
			}
			if _, duplicate := aliases[entry.ItemID]; duplicate {
				return fmt.Errorf("GM item image alias repeats item %d", entry.ItemID)
			}
			if entry.CanonicalItemID != 0 {
				if entry.CanonicalItemID == entry.ItemID {
					return fmt.Errorf("GM item image alias %d targets itself", entry.ItemID)
				}
				if _, known := server.itemByID[entry.CanonicalItemID]; !known {
					return fmt.Errorf("GM item image alias %d targets unknown canonical item %d", entry.ItemID, entry.CanonicalItemID)
				}
				aliases[entry.ItemID] = itemImageAlias{targetItemID: entry.CanonicalItemID}
				continue
			}
			relative := filepath.Clean(filepath.FromSlash(entry.RelativePath))
			if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("GM item image alias %d has unsafe path %q", entry.ItemID, entry.RelativePath)
			}
			absolute, err := filepath.Abs(filepath.Join(root, relative))
			if err != nil {
				return err
			}
			within, err := filepath.Rel(root, absolute)
			if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
				return fmt.Errorf("GM item image alias %d escapes client root", entry.ItemID)
			}
			imageData, err := os.ReadFile(absolute)
			if err != nil {
				server.imageWarnings = append(server.imageWarnings,
					fmt.Sprintf("GM item image alias %d was skipped: %v", entry.ItemID, err))
				continue
			}
			err = itemcatalog.ValidateDIMG(imageData, entry.TightFrame)
			if err != nil {
				server.imageWarnings = append(server.imageWarnings,
					fmt.Sprintf("GM item image alias %d was skipped because it is not a decodable DIMG: %v", entry.ItemID, err))
				continue
			}
			aliases[entry.ItemID] = itemImageAlias{ref: itemImageRef{path: absolute, source: "explicit-alias", tightFrame: entry.TightFrame}}
		}
		ids := make([]uint32, 0, len(aliases))
		for id := range aliases {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			alias := aliases[id]
			if alias.targetItemID == 0 {
				continue
			}
			if _, aliasTarget := aliases[alias.targetItemID]; aliasTarget {
				return fmt.Errorf("GM item image alias %d targets another alias %d", id, alias.targetItemID)
			}
			target := server.itemByID[alias.targetItemID]
			if _, ok := server.resolveItemImageBase(target, 7); !ok {
				server.imageWarnings = append(server.imageWarnings,
					fmt.Sprintf("GM item image alias %d was skipped because canonical item %d has no exact local image", id, alias.targetItemID))
				delete(aliases, id)
			}
		}
		server.imageAliases = aliases
		return nil
	}
}

func (server *Server) resolveItemImage(item itemcatalog.Entry, preferredRole byte) (itemImageRef, bool) {
	if alias, ok := server.imageAliases[item.ID]; ok {
		if alias.targetItemID == 0 {
			return alias.ref, true
		}
		target := server.itemByID[alias.targetItemID]
		ref, found := server.resolveItemImageBase(target, preferredRole)
		if found {
			ref.source = "canonical-alias"
		}
		return ref, found
	}
	return server.resolveItemImageBase(item, preferredRole)
}

func preferredPetModelImage(level1, level4, level7 uint32) uint32 {
	// Level 1 deliberately uses the shared pet100 placeholder. A species'
	// juvenile (level 4) model is the first useful identity preview; fall back
	// to the adult or shared model only when the corresponding tier is absent.
	if level4 != 0 {
		return level4
	}
	if level7 != 0 {
		return level7
	}
	return level1
}

func (server *Server) resolvePetModelImage(modelID uint32) (itemImageRef, bool) {
	if modelID == 0 {
		return itemImageRef{}, false
	}
	icon := filepath.Join(server.clientRoot, "res", "uiRes", "icon", "pet", fmt.Sprintf("pet%d.img", modelID))
	if regularFile(icon) {
		return itemImageRef{path: icon, source: "pet-icon", tightFrame: true}, true
	}
	stand := filepath.Join(server.clientRoot, "object", "pet", fmt.Sprintf("pet%d_stand.img", modelID))
	if regularFile(stand) {
		return itemImageRef{path: stand, source: "pet-model", tightFrame: true}, true
	}
	return itemImageRef{}, false
}

func (server *Server) resolveItemImageBase(item itemcatalog.Entry, preferredRole byte) (itemImageRef, bool) {
	category := authoritativeResourceCategory(item)
	// item<ID>.img belongs to the account registry's "item" resource category.
	// Numeric IDs alone are not identities: frame/3 with protocol ID 31 is not
	// the match-scene item/31 fork that happens to share the number. Materials
	// and recipes are catalogued by XML rather than itemCFG.py, so their
	// RegistryCategory is empty even though the client inventory deliberately
	// addresses their icons as item<ID>.img.
	if usesAccountItemIcon(item) {
		itemIcon := filepath.Join(server.clientRoot, "res", "uiRes", "icon", "item", fmt.Sprintf("item%d.img", item.ID))
		if regularFile(itemIcon) {
			// Win_SetImg renders the actual DIMG frames. The sprite header's
			// declared canvas and frame_info_cx/cy are not reliable UI bounds;
			// some original icons extend beyond them. Use the same frame-based
			// preview path as the client instead of rejecting or shrinking them.
			return itemImageRef{path: itemIcon, source: "item-icon", tightFrame: true}, true
		}
	}
	if category != "" && item.Index != 0 {
		iconDirectory, iconStem := category, fmt.Sprintf("%s%d", category, item.Index)
		// The official manifests are split: resources 1-2 predate the
		// dedicated namecardbound directories, while 3-5 use them.
		if category == "namecardbound" && item.Index <= 2 {
			iconDirectory = "namecard"
		}
		categoryIcon := filepath.Join(server.clientRoot, "res", "uiRes", "icon", iconDirectory, iconStem+".img")
		if regularFile(categoryIcon) {
			return itemImageRef{path: categoryIcon, source: "category-icon", tightFrame: true}, true
		}
	}
	return server.resolveItemAppearance(item, preferredRole)
}

// resolveItemAppearance follows the client's equipped-object path. It is
// intentionally separate from resolveItemImageBase, whose first choice is the
// icon path used by shop/inventory widgets. Account rows marked as equipped
// may request this representation without changing the canonical catalog icon.
func (server *Server) resolveItemAppearance(item itemcatalog.Entry, preferredRole byte) (itemImageRef, bool) {
	for _, candidate := range appearanceCandidates(item, preferredRole) {
		loosePath := filepath.Join(append([]string{server.clientRoot}, strings.Split(candidate.path, `\`)...)...)
		if regularFile(loosePath) {
			return itemImageRef{path: loosePath, source: "appearance-layer", tightFrame: true, availableRole: candidate.role}, true
		}
		if server.objectArchive != nil && server.objectArchive.Has(candidate.path) {
			return itemImageRef{archivePath: candidate.path, source: "appearance-layer", tightFrame: true, availableRole: candidate.role}, true
		}
	}
	return itemImageRef{}, false
}

func usesAccountItemIcon(item itemcatalog.Entry) bool {
	if item.RegistryCategory == "item" {
		return true
	}
	switch item.Kind {
	case "account-item", "inventory-consumable", "material", "craft-recipe", "forge-gem":
		return true
	default:
		return false
	}
}

func (server *Server) readItemImage(ref itemImageRef) ([]byte, error) {
	if ref.path != "" {
		return os.ReadFile(ref.path)
	}
	return server.objectArchive.Read(ref.archivePath)
}

// ValidateItemImages is an explicit diagnostic pass. Production startup keeps
// it off the readiness path because walking and decoding the full client image
// catalog is optional GM work and is especially expensive on virtual disks.
// Normal HTTP image requests still decode lazily and use a fallback on failure.
func (server *Server) ValidateItemImages() error {
	validated := make(map[string]error)
	issues := make([]string, 0)
	validate := func(item itemcatalog.Entry, label string, ref itemImageRef) {
		key := fmt.Sprintf("%s\x00%s\x00%t", ref.path, ref.archivePath, ref.tightFrame)
		err, checked := validated[key]
		if !checked {
			var data []byte
			data, err = server.readItemImage(ref)
			if err == nil {
				err = itemcatalog.ValidateDIMG(data, ref.tightFrame)
			}
			validated[key] = err
		}
		if err != nil {
			issues = append(issues, fmt.Sprintf("%d%s:%v", item.ID, label, err))
		}
	}
	for _, item := range server.items {
		ref, ok := server.resolveItemImage(item, 7)
		if !ok {
			issues = append(issues, fmt.Sprintf("%d:no exact resource", item.ID))
			continue
		}
		validate(item, "", ref)
		// Appearance is optional because many non-wearable inventory entries
		// legitimately have only an icon. When an exact equipped layer exists,
		// however, validate it too so account rows cannot regress to a placeholder.
		if appearance, found := server.resolveItemAppearance(item, 7); found {
			validate(item, " appearance", appearance)
		}
	}
	if len(issues) != 0 {
		const detailLimit = 12
		details := issues
		if len(details) > detailLimit {
			details = details[:detailLimit]
		}
		return fmt.Errorf("GM item images failed strict validation for %d/%d items: %s", len(issues), len(server.items), strings.Join(details, "; "))
	}
	return nil
}

type appearanceCandidate struct {
	path string
	role byte
}

func appearanceCandidates(item itemcatalog.Entry, preferredRole byte) []appearanceCandidate {
	if item.Index == 0 {
		return nil
	}
	roles := make([]byte, 0, 1)
	if preferredRole >= 1 && preferredRole <= 22 {
		roles = append(roles, preferredRole)
	}
	seen := make(map[string]struct{})
	result := make([]appearanceCandidate, 0, 32)
	add := func(category, stem string, role byte) {
		path := fmt.Sprintf(`object\%s\%s_stand.img`, category, stem)
		key := strings.ToLower(path)
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		result = append(result, appearanceCandidate{path: path, role: role})
	}
	for _, category := range []string{authoritativeResourceCategory(item)} {
		if category == "" {
			continue
		}
		// Many FilePath manifests install one shared appearance layer with the
		// exact category/resource stem (eye37_stand, cladorn187_stand, ...).
		// Check that evidence-backed path before older category-specific naming
		// conventions. Missing files simply fall through to the documented forms.
		add(category, fmt.Sprintf("%s%d", category, item.Index), 0)
		switch category {
		case "cap":
			// Early hats use one shared cap10N layer; later hats can have a
			// role-specific cap10RNN layer.
			add("cap", fmt.Sprintf("cap%d", 100+item.Index), 0)
			for _, role := range roles {
				add("cap", fmt.Sprintf("cap%d%02d", 100+role, item.Index), role)
			}
		case "thadorn":
			add("thadorn", fmt.Sprintf("thadorn%d", 100+item.Index), 0)
			add("thadorn", fmt.Sprintf("thadorn%d", item.Index), 0)
		case "fpack":
			for _, role := range roles {
				add("fpack", fmt.Sprintf("fpack%d%02d", 100+role, item.Index), role)
			}
		case "cladorn":
			for _, role := range roles {
				add("cladorn", fmt.Sprintf("cladorn%d%02d", item.Index, role), role)
			}
		case "cloth", "body":
			for _, role := range roles {
				add(category, fmt.Sprintf("%s%d%02d", category, item.Index, role), role)
			}
		case "hair", "leg", "mouth", "npack":
			for _, role := range roles {
				add(category, fmt.Sprintf("%s%d%02d", category, 100+role, item.Index), role)
			}
			add(category, fmt.Sprintf("%s%d", category, item.Index), 0)
		case "namecardbound":
			objectCategory := "namecardbound"
			if item.Index <= 2 {
				objectCategory = "namecard"
			}
			add(objectCategory, fmt.Sprintf("namecardbound%d", item.Index), 0)
		case "bomb", "foot", "footprint", "namecard", "bg", "frame", "enter", "huanying":
			add(category, fmt.Sprintf("%s%d", category, item.Index), 0)
		}
	}
	return result
}

func authoritativeResourceCategory(item itemcatalog.Entry) string {
	if item.RegistryCategory != "" {
		return item.RegistryCategory
	}
	if len(item.Categories) == 1 {
		return item.Categories[0]
	}
	return ""
}

func accountResourceKey(item itemcatalog.Entry) string {
	category := authoritativeResourceCategory(item)
	if category != "" && item.Index != 0 {
		return fmt.Sprintf("account/%s/%d", category, item.Index)
	}
	return fmt.Sprintf("account/id/%d", item.ID)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
