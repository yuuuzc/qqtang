package shopcatalog

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"qqtang/internal/game/itemcatalog"
)

// Commodity.ini uses the packed major/subtype values consumed by the
// client's fetchCommodity(type, subtype, index) API. The mapping follows the
// category labels set by uiShop.py (including 酷比宝石 and 锻造水晶).
var categoryTypes = map[string]uint16{
	"platform":      0x0201,
	"card":          0x0202,
	"item":          0x0203,
	"kubi":          0x0204,
	"forge":         0x0205,
	"bomb":          0x0301,
	"huanying":      0x0302,
	"footprint":     0x0303,
	"bg":            0x0304,
	"frame":         0x0304,
	"enter":         0x0304,
	"namecard":      0x0305,
	"namecardbound": 0x0305,
	"thadorn":       0x0401,
	"cap":           0x0401,
	"hair":          0x0401,
	"eye":           0x0402,
	"mouth":         0x0402,
	"fpack":         0x0403,
	"npack":         0x0403,
	"ear":           0x0404,
	"cladorn":       0x0404,
	"food":          0x0501,
	"skill":         0x0502,
	"petcard":       0x0503,
}

// ValidateCommodityINI proves that the file consumed by QQTShop2ND is the
// exact catalog the server is about to publish. Startup generates the file
// once, before the server starts; clients never rewrite the shared file while
// another instance or the native shop cache may be reading it.
func ValidateCommodityINI(path string, version uint32, items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry, limit int) error {
	want, summary, err := BuildCommodityINIWithLimit(version, items, commodities, limit)
	if err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read generated Commodity.ini: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("Commodity.ini does not match published catalog version %d with %d commodities", summary.Version, summary.CommodityCount)
	}
	return nil
}

// DefaultCommodityLimit publishes the complete client commodity registry.
// A non-zero limit remains available to diagnostic tools, but production
// generation must not hide valid original storefront rows.
const DefaultCommodityLimit = 0

type Row struct {
	Commodity itemcatalog.CommodityEntry
	ItemID    uint32
	Type      uint16
}

type Summary struct {
	Version        uint32
	CommodityCount int
	ItemCount      int
}

// BuildCommodityINI joins storefront identities to canonical account item IDs
// through their category-local resource identity. Commodity IDs and item IDs
// are deliberately never equated.
func BuildCommodityINI(version uint32, items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry) ([]byte, Summary, error) {
	return BuildCommodityINIWithLimit(version, items, commodities, DefaultCommodityLimit)
}

func BuildCommodityINIWithLimit(version uint32, items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry, limit int) ([]byte, Summary, error) {
	if version == 0 {
		return nil, Summary{}, fmt.Errorf("commodity version must be non-zero")
	}
	rows, err := buildRows(items, commodities, limit)
	if err != nil {
		return nil, Summary{}, err
	}

	var utf8 bytes.Buffer
	// Preserve the CR-only convention used by the original 5.2 configuration
	// files even though the native reader accepts either CR or LF delimiters.
	fmt.Fprintf(&utf8, "[public]\rversion = %d\r\r", version)
	fmt.Fprintf(&utf8, "[COMMODITY]\rCOMMODITYCOUNT = %d\r\r", len(rows))
	for index, row := range rows {
		// commodityCFG does not contain the retired historical price table. A
		// small positive local Q-price keeps the original card layout and buy
		// path active; local purchase settlement remains server-authoritative.
		fmt.Fprintf(&utf8, "[COMMODITY.%d]\r", index+1)
		fmt.Fprintf(&utf8, "COMMODITYID = %d\r", row.Commodity.CommodityID)
		fmt.Fprintf(&utf8, "COMMODITYNAME = %s\r", sanitizeINIValue(row.Commodity.Name))
		fmt.Fprintf(&utf8, "COMMODITYTYPE = %d\r", row.Type)
		fmt.Fprintf(&utf8, "ITEMCOUNT = 1\rQPRICE = %d\rQQPOINTPRICE = %d\rQQTANGPRICE = %d\rRESTORPRICE = 0\rSALEDATELIMIT = 0\rMEMBERREBATE = 100\rATTRIBUTE = 0\rREBATE = 100\r", LocalQPrice, LocalQQPointPrice, LocalSugarPrice)
		fmt.Fprintf(&utf8, "ITEM.1.ID = %d\rITEM.1.NUM = %d\rITEM.1.AVAILPERIOD = 0\r\r", row.ItemID, commodityBundleQuantity(row.Commodity.Name))
	}
	writeRecommendations(&utf8, rows)
	encoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), utf8.Bytes())
	if err != nil {
		return nil, Summary{}, fmt.Errorf("encode Commodity.ini as GBK: %w", err)
	}
	return encoded, Summary{Version: version, CommodityCount: len(rows), ItemCount: len(items)}, nil
}

// selectEvenlyByCategory walks every original resource category in rounds.
// Small categories are exhausted first and their unused quota is naturally
// redistributed, while large cosmetic families cannot crowd every other kind
// out of the storefront.
func selectEvenlyByCategory(rows []Row, limit int) []Row {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	groups := make(map[string][]Row)
	for _, row := range rows {
		category := strings.ToLower(strings.TrimSpace(row.Commodity.Category))
		groups[category] = append(groups[category], row)
	}
	categories := make([]string, 0, len(groups))
	for category := range groups {
		categories = append(categories, category)
		sort.Slice(groups[category], func(i, j int) bool {
			return groups[category][i].Commodity.CommodityID < groups[category][j].Commodity.CommodityID
		})
	}
	sort.Strings(categories)
	cursors := make(map[string]int, len(categories))
	selected := make([]Row, 0, limit)
	for len(selected) < limit {
		progress := false
		for _, category := range categories {
			index := cursors[category]
			if index >= len(groups[category]) {
				continue
			}
			selected = append(selected, groups[category][index])
			cursors[category] = index + 1
			progress = true
			if len(selected) == limit {
				break
			}
		}
		if !progress {
			break
		}
	}
	return selected
}

func writeRecommendations(output *bytes.Buffer, rows []Row) {
	groups := [4][]uint32{}
	appendMatching := func(group int, categories map[string]bool, reverse bool) {
		for offset := 0; offset < len(rows) && len(groups[group]) < 12; offset++ {
			index := offset
			if reverse {
				index = len(rows) - 1 - offset
			}
			row := rows[index]
			if categories[row.Commodity.Category] {
				groups[group] = append(groups[group], row.Commodity.CommodityID)
			}
		}
	}
	cosmetics := map[string]bool{
		"bomb": true, "huanying": true, "footprint": true, "bg": true,
		"frame": true, "enter": true, "namecard": true, "namecardbound": true,
		"thadorn": true, "cap": true, "hair": true, "eye": true,
		"mouth": true, "fpack": true, "npack": true, "ear": true, "cladorn": true,
	}
	appendMatching(0, cosmetics, true)  // 新品推荐
	appendMatching(1, cosmetics, false) // 低价促销
	appendMatching(2, map[string]bool{"thadorn": true, "namecard": true, "namecardbound": true}, true)
	appendMatching(3, map[string]bool{"platform": true, "card": true, "forge": true, "item": true, "skill": true}, false)
	fmt.Fprint(output, "[comment]\rcount = 4\r\r")
	for group, ids := range groups {
		fmt.Fprintf(output, "[comment.%d]\rcount = 1\ritem.1 = ", group+1)
		for index, id := range ids {
			if index != 0 {
				output.WriteByte(',')
			}
			fmt.Fprint(output, id)
		}
		fmt.Fprint(output, "\r\r")
	}
}

func sanitizeINIValue(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "=", "-").Replace(value))
}
