package itemcatalog

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"qqtang/internal/clientdata/sceneelement"
)

type Entry struct {
	ID                          uint32   `json:"id"`
	Index                       uint32   `json:"index,omitempty"`
	RegistryCategory            string   `json:"registry_category,omitempty"`
	Name                        string   `json:"name"`
	Kind                        string   `json:"kind"`
	Categories                  []string `json:"categories,omitempty"`
	Description                 string   `json:"description,omitempty"`
	ClientSceneFactorySupported bool     `json:"client_scene_factory_supported"`
	SourceFiles                 []string `json:"source_files"`
	Confidence                  string   `json:"confidence"`
}

type parsedEntry struct {
	ID          uint32
	Index       uint32
	Name        string
	Category    string
	Description string
}

// RegistryEntry is one canonical row from object/itemCFG.py. ItemID is the
// dictionary key used by the game protocol and account inventory. ResourceID
// is the category-local identifier used to locate downloadable art; the two
// identifiers are deliberately not interchangeable.
type RegistryEntry struct {
	ItemID      uint32 `json:"item_id"`
	Category    string `json:"category"`
	ResourceID  uint32 `json:"resource_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CommodityEntry is one storefront row from object/commodityCFG.py.
// CommodityID is a shop/product identity, never an account inventory item ID.
type CommodityEntry struct {
	CommodityID uint32 `json:"commodity_id"`
	Category    string `json:"category"`
	ResourceID  uint32 `json:"resource_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Load extracts the installed client's account-item catalog. The factory flag
// only states that the in-match scene factory recognizes the same numeric ID;
// it does not by itself prove that a map or monster can drop that item.
func Load(clientRoot string) ([]Entry, error) {
	type source struct {
		name string
		path string
	}
	sources := []source{
		{name: "config/propdescrip.xml", path: filepath.Join(clientRoot, "config", "propdescrip.xml")},
		{name: "config/DealProp2.xml", path: filepath.Join(clientRoot, "config", "DealProp2.xml")},
	}
	entries := make(map[uint32]*Entry)
	itemCFGPath := filepath.Join(clientRoot, "object", "itemCFG.py")
	itemCFGEntries, err := parseItemCFG(itemCFGPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("parse object/itemCFG.py: %w", err)
	}
	for _, item := range itemCFGEntries {
		entry := entries[item.ID]
		if entry == nil {
			entry = &Entry{ID: item.ID}
			entries[item.ID] = entry
		}
		entry.Name = item.Name
		entry.Description = item.Description
		entry.Index = item.Index
		entry.RegistryCategory = item.Category
		entry.Categories = appendUnique(entry.Categories, item.Category)
		entry.SourceFiles = appendUnique(entry.SourceFiles, "object/itemCFG.py")
	}
	for _, source := range sources {
		parsed, err := parseXML(source.path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", source.name, err)
		}
		for _, item := range parsed {
			entry := entries[item.ID]
			if entry == nil {
				entry = &Entry{ID: item.ID}
				entries[item.ID] = entry
			}
			// DealProp2.xml is newer in this client (version 110 versus 108),
			// so processing it second makes its wording authoritative.
			entry.Name = item.Name
			if item.Description != "" {
				entry.Description = item.Description
			}
			entry.Categories = appendUnique(entry.Categories, item.Category)
			entry.SourceFiles = appendUnique(entry.SourceFiles, source.name)
		}
	}

	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		sort.Strings(entry.Categories)
		sort.Strings(entry.SourceFiles)
		entry.Kind = classify(*entry)
		entry.ClientSceneFactorySupported = sceneelement.IsClientID(entry.ID)
		entry.Confidence = "confirmed-client-config"
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

var (
	registryVersionPattern = regexp.MustCompile(`^\s*version\s*=\s*([0-9]+)\s*$`)
	itemCFGEntryPattern    = regexp.MustCompile(`^\s*([0-9]+):\('([^']+)',\s*([0-9]+),\s*'([^']*)',\s*'([^']*)'`)
)

// LoadItemRegistry reads only itemCFG.itemList. Unlike Load, it does not add
// XML-only descriptions as synthetic item IDs, so the returned ItemID set is
// the canonical account/protocol registry.
func LoadItemRegistry(clientRoot string) (uint32, []RegistryEntry, error) {
	version, parsed, err := parseRegistry(filepath.Join(clientRoot, "object", "itemCFG.py"), "itemList")
	if err != nil {
		return 0, nil, err
	}
	result := make([]RegistryEntry, 0, len(parsed))
	for _, entry := range parsed {
		result = append(result, RegistryEntry{
			ItemID: entry.ID, Category: entry.Category, ResourceID: entry.Index,
			Name: entry.Name, Description: entry.Description,
		})
	}
	return version, result, nil
}

// LoadCommodityRegistry reads only commodityCFG.commodityList and preserves
// the storefront dictionary key separately from category-local ResourceID.
func LoadCommodityRegistry(clientRoot string) (uint32, []CommodityEntry, error) {
	version, parsed, err := parseRegistry(filepath.Join(clientRoot, "object", "commodityCFG.py"), "commodityList")
	if err != nil {
		return 0, nil, err
	}
	// The recovered Python source contains a small number of repeated dict
	// keys. Python keeps the final value for such a key, so mirror that exact
	// behavior instead of exposing impossible duplicate storefront identities.
	lastByID := make(map[uint32]parsedEntry, len(parsed))
	for _, entry := range parsed {
		lastByID[entry.ID] = entry
	}
	ids := make([]uint32, 0, len(lastByID))
	for id := range lastByID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]CommodityEntry, 0, len(ids))
	for _, id := range ids {
		entry := lastByID[id]
		result = append(result, CommodityEntry{
			CommodityID: entry.ID, Category: entry.Category, ResourceID: entry.Index,
			Name: entry.Name, Description: entry.Description,
		})
	}
	return version, result, nil
}

// parseItemCFG reads the client's complete 2,945-entry item registry. Unlike
// the smaller store XMLs, this table includes backgrounds, avatar cosmetics,
// pet food, and the distinct pet-card IDs used by scene drops.
func parseItemCFG(path string) ([]parsedEntry, error) {
	_, result, err := parseRegistry(path, "itemList")
	return result, err
}

func parseRegistry(path, tableName string) (uint32, []parsedEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(transform.NewReader(file, simplifiedchinese.GBK.NewDecoder()))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var result []parsedEntry
	var version uint32
	for scanner.Scan() {
		line := scanner.Text()
		if versionMatch := registryVersionPattern.FindStringSubmatch(line); versionMatch != nil {
			parsedVersion, parseErr := strconv.ParseUint(versionMatch[1], 10, 32)
			if parseErr != nil {
				return 0, nil, fmt.Errorf("invalid %s version %q: %w", tableName, versionMatch[1], parseErr)
			}
			version = uint32(parsedVersion)
		}
		match := itemCFGEntryPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		id, parseErr := strconv.ParseUint(match[1], 10, 32)
		if parseErr != nil {
			return 0, nil, fmt.Errorf("invalid %s ID %q: %w", tableName, match[1], parseErr)
		}
		index, parseErr := strconv.ParseUint(match[3], 10, 32)
		if parseErr != nil {
			return 0, nil, fmt.Errorf("invalid %s resource ID %q for entry %d: %w", tableName, match[3], id, parseErr)
		}
		result = append(result, parsedEntry{
			ID: uint32(id), Index: uint32(index), Name: match[4], Category: match[2], Description: match[5],
		})
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, err
	}
	if len(result) == 0 {
		return 0, nil, fmt.Errorf("registry contains no %s entries", tableName)
	}
	if version == 0 {
		return 0, nil, fmt.Errorf("registry contains no %s version", tableName)
	}
	return version, result, nil
}

func parseXML(path string) ([]parsedEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "gb2312", "gbk", "cp936", "windows-936":
			return transform.NewReader(input, simplifiedchinese.GBK.NewDecoder()), nil
		default:
			return nil, fmt.Errorf("unsupported XML charset %q", label)
		}
	}
	var result []parsedEntry
	var categories []string
	depth := 0
	for {
		token, tokenErr := decoder.Token()
		if tokenErr == io.EOF {
			break
		}
		if tokenErr != nil {
			return nil, tokenErr
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			category := ""
			if depth == 2 {
				category = attribute(value.Attr, "name")
				if category == "" {
					category = value.Name.Local
				}
			}
			categories = append(categories, category)
			if strings.EqualFold(value.Name.Local, "type") {
				continue
			}
			idText := attribute(value.Attr, "id")
			name := strings.TrimSpace(attribute(value.Attr, "name"))
			if idText == "" || name == "" {
				continue
			}
			id, parseErr := strconv.ParseUint(idText, 10, 32)
			if parseErr != nil {
				return nil, fmt.Errorf("element %s has invalid id %q: %w", value.Name.Local, idText, parseErr)
			}
			result = append(result, parsedEntry{
				ID:          uint32(id),
				Name:        name,
				Category:    nearestCategory(categories),
				Description: strings.TrimSpace(attribute(value.Attr, "description")),
			})
		case xml.EndElement:
			if len(categories) > 0 {
				categories = categories[:len(categories)-1]
			}
			depth--
		}
	}
	return result, nil
}

func attribute(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute.Name.Local, name) {
			return attribute.Value
		}
	}
	return ""
}

func nearestCategory(categories []string) string {
	for index := len(categories) - 1; index >= 0; index-- {
		if categories[index] != "" {
			return categories[index]
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func classify(entry Entry) string {
	if entry.RegistryCategory != "" {
		switch entry.RegistryCategory {
		case "food":
			return "pet-food"
		case "petcard":
			return "pet-card"
		case "bg", "frame", "enter", "namecard", "namecardbound":
			return "profile-decoration"
		case "thadorn", "cap", "hair", "eye", "mouth", "fpack", "npack", "ear", "cladorn":
			return "avatar-cosmetic"
		}
	}
	categories := strings.Join(entry.Categories, " ")
	switch {
	case containsCategory(entry.Categories, "food"):
		return "pet-food"
	case containsCategory(entry.Categories, "petcard"):
		return "pet-card"
	case containsAnyCategory(entry.Categories, "bg", "frame", "enter", "namecard", "namecardbound"):
		return "profile-decoration"
	case containsAnyCategory(entry.Categories, "thadorn", "cap", "hair", "eye", "mouth", "fpack", "npack", "ear", "cladorn"):
		return "avatar-cosmetic"
	case strings.Contains(categories, "宠物"):
		return "pet-card"
	case strings.Contains(categories, "技能书"):
		return "pet-skill-book"
	case strings.Contains(categories, "合成书"):
		return "craft-recipe"
	case strings.Contains(categories, "材料"):
		return "material"
	case strings.Contains(categories, "宝石"):
		return "forge-gem"
	case strings.Contains(categories, "场用品") || entry.ID >= 20000 && entry.ID < 20200:
		return "inventory-consumable"
	default:
		return "account-item"
	}
}

func containsCategory(categories []string, want string) bool {
	for _, category := range categories {
		if category == want {
			return true
		}
	}
	return false
}

func containsAnyCategory(categories []string, wants ...string) bool {
	for _, want := range wants {
		if containsCategory(categories, want) {
			return true
		}
	}
	return false
}
