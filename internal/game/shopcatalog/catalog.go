package shopcatalog

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"qqtang/internal/game/itemcatalog"
)

var commodityBundleQuantityPattern = regexp.MustCompile(`[\[\(（]\s*([0-9]+)\s*个`)

const (
	LocalQPrice       uint32 = 10
	LocalQQPointPrice uint32 = 10
	LocalSugarPrice   uint32 = 1000
)

// Product is the server-authoritative counterpart of one Commodity.ini row.
// CommodityID identifies the storefront offer; ItemID is the canonical item
// granted to the account. They are intentionally separate types and fields.
type Product struct {
	CommodityID     uint32
	ItemID          uint16
	Category        string
	Name            string
	Description     string
	Type            uint16
	Quantity        uint32
	AvailablePeriod uint32
	QPrice          uint32
	QQPointPrice    uint32
	SugarPrice      uint32
}

// Catalog contains exactly the products published in the generated
// Commodity.ini, so a visible product can never be absent from settlement.
type Catalog struct {
	version  uint32
	products []Product
	byID     map[uint32]Product
}

func NewCatalog(version uint32, items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry, limit int) (*Catalog, error) {
	rows, err := buildRows(items, commodities, limit)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, fmt.Errorf("commodity version must be non-zero")
	}
	products := make([]Product, 0, len(rows))
	byID := make(map[uint32]Product, len(rows))
	for _, row := range rows {
		if row.ItemID == 0 || row.ItemID > math.MaxUint16 {
			return nil, fmt.Errorf("commodity %d canonical item ID %d is outside account inventory range", row.Commodity.CommodityID, row.ItemID)
		}
		product := Product{
			CommodityID:     row.Commodity.CommodityID,
			ItemID:          uint16(row.ItemID),
			Category:        strings.ToLower(strings.TrimSpace(row.Commodity.Category)),
			Name:            row.Commodity.Name,
			Description:     row.Commodity.Description,
			Type:            row.Type,
			Quantity:        commodityBundleQuantity(row.Commodity.Name),
			AvailablePeriod: 0,
			QPrice:          LocalQPrice,
			QQPointPrice:    LocalQQPointPrice,
			SugarPrice:      LocalSugarPrice,
		}
		products = append(products, product)
		byID[product.CommodityID] = product
	}
	return &Catalog{version: version, products: products, byID: byID}, nil
}

// commodityBundleQuantity extracts the storefront bundle count used by the
// original commodity table (for example "大体力药水[100个"). It deliberately
// requires an opening delimiter before the number, so names such as
// "10格宠物栏" and descriptions mentioning recipe requirements remain one
// purchased inventory item.
func commodityBundleQuantity(name string) uint32 {
	match := commodityBundleQuantityPattern.FindStringSubmatch(name)
	if len(match) != 2 {
		return 1
	}
	quantity, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil || quantity == 0 {
		return 1
	}
	return uint32(quantity)
}

func (catalog *Catalog) Version() uint32 {
	if catalog == nil {
		return 0
	}
	return catalog.version
}

func (catalog *Catalog) Lookup(commodityID uint32) (Product, bool) {
	if catalog == nil {
		return Product{}, false
	}
	product, ok := catalog.byID[commodityID]
	return product, ok
}

func (catalog *Catalog) Products() []Product {
	if catalog == nil {
		return nil
	}
	return append([]Product(nil), catalog.products...)
}

func buildRows(items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry, limit int) ([]Row, error) {
	type resourceKey struct {
		category string
		id       uint32
	}
	itemByResource := make(map[resourceKey][]uint32, len(items))
	for _, item := range items {
		key := resourceKey{category: strings.ToLower(strings.TrimSpace(item.Category)), id: item.ResourceID}
		itemByResource[key] = append(itemByResource[key], item.ItemID)
	}
	rows := make([]Row, 0, len(commodities))
	seenCommodity := make(map[uint32]struct{}, len(commodities))
	for _, commodity := range commodities {
		if _, duplicate := seenCommodity[commodity.CommodityID]; duplicate {
			return nil, fmt.Errorf("duplicate commodity ID %d", commodity.CommodityID)
		}
		seenCommodity[commodity.CommodityID] = struct{}{}
		category := strings.ToLower(strings.TrimSpace(commodity.Category))
		packedType, known := categoryTypes[category]
		if !known {
			return nil, fmt.Errorf("commodity %d uses unknown category %q", commodity.CommodityID, commodity.Category)
		}
		matches := itemByResource[resourceKey{category: category, id: commodity.ResourceID}]
		itemID := uint32(0)
		if len(matches) == 1 {
			itemID = matches[0]
		} else if len(matches) > 1 {
			// Local extension items may deliberately share an original resource
			// image. An exact commodity/item identity disambiguates that case;
			// historical bundle commodities without such an identity remain an
			// error instead of silently granting the wrong canonical item.
			for _, candidate := range matches {
				if candidate == commodity.CommodityID {
					itemID = candidate
					break
				}
			}
			if itemID == 0 {
				// Original duration bundles use a different CommodityID while
				// retaining the canonical item name as a prefix (for example
				// 单人探险卡[7日). Use that proven identity only when unique.
				for _, item := range items {
					if item.Category != commodity.Category || item.ResourceID != commodity.ResourceID || item.Name == "" || !strings.HasPrefix(commodity.Name, item.Name) {
						continue
					}
					if itemID != 0 {
						itemID = 0
						break
					}
					itemID = item.ItemID
				}
			}
		}
		if itemID == 0 {
			return nil, fmt.Errorf("commodity %d resource %s/%d maps to %d canonical items", commodity.CommodityID, category, commodity.ResourceID, len(matches))
		}
		rows = append(rows, Row{Commodity: commodity, ItemID: itemID, Type: packedType})
	}
	rows = selectEvenlyByCategory(rows, limit)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Commodity.CommodityID < rows[j].Commodity.CommodityID
	})
	return rows, nil
}
