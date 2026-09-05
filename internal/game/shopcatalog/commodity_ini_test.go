package shopcatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"qqtang/internal/game/itemcatalog"
)

func TestBuildCommodityINIUsesCanonicalItemIDAndPackedType(t *testing.T) {
	items := []itemcatalog.RegistryEntry{{ItemID: 2067, Category: "cladorn", ResourceID: 187, Name: "中山装"}}
	commodities := []itemcatalog.CommodityEntry{{CommodityID: 912067, Category: "cladorn", ResourceID: 187, Name: "中山装[30天]"}}
	data, summary, err := BuildCommodityINI(848, items, commodities)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\n") {
		t.Fatal("Commodity.ini must use the legacy client's CR-only record separator")
	}
	if !strings.Contains(string(data), "\r") {
		t.Fatal("Commodity.ini contains no CR record separators")
	}
	decoded, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), string(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"version = 848", "COMMODITYCOUNT = 1", "[COMMODITY.1]", "COMMODITYID = 912067",
		"COMMODITYNAME = 中山装[30天]", "COMMODITYTYPE = 1028", "QPRICE = 10", "QQPOINTPRICE = 10", "QQTANGPRICE = 1000",
		"ITEM.1.ID = 2067", "ITEM.1.NUM = 1", "ITEM.1.AVAILPERIOD = 0",
		"[comment]", "count = 4", "[comment.1]", "item.1 = 912067",
	} {
		if !strings.Contains(decoded, want) {
			t.Fatalf("Commodity.ini does not contain %q:\n%s", want, decoded)
		}
	}
	if summary.CommodityCount != 1 || summary.ItemCount != 1 || summary.Version != 848 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestValidateCommodityINIRejectsAStalePublishedFile(t *testing.T) {
	items := []itemcatalog.RegistryEntry{{ItemID: 2067, Category: "cladorn", ResourceID: 187, Name: "中山装"}}
	commodities := []itemcatalog.CommodityEntry{{CommodityID: 912067, Category: "cladorn", ResourceID: 187, Name: "中山装[30天]"}}
	data, _, err := BuildCommodityINIWithLimit(848, items, commodities, DefaultCommodityLimit)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Commodity.ini")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCommodityINI(path, 848, items, commodities, DefaultCommodityLimit); err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCommodityINI(path, 848, items, commodities, DefaultCommodityLimit); err == nil || !strings.Contains(err.Error(), "does not match published catalog") {
		t.Fatalf("stale catalog validation error = %v", err)
	}
}

func TestBuildCommodityINIRejectsAmbiguousResource(t *testing.T) {
	items := []itemcatalog.RegistryEntry{
		{ItemID: 1, Category: "cap", ResourceID: 3},
		{ItemID: 2, Category: "cap", ResourceID: 3},
	}
	_, _, err := BuildCommodityINI(848, items, []itemcatalog.CommodityEntry{{CommodityID: 9, Category: "cap", ResourceID: 3}})
	if err == nil || !strings.Contains(err.Error(), "maps to 2 canonical items") {
		t.Fatalf("error = %v", err)
	}
}

func TestCatalogMatchesPublishedCommodityRows(t *testing.T) {
	items := []itemcatalog.RegistryEntry{
		{ItemID: 2067, Category: "cladorn", ResourceID: 187, Name: "中山装"},
		{ItemID: 20043, Category: "item", ResourceID: 15, Name: "大体力药水"},
	}
	commodities := []itemcatalog.CommodityEntry{
		{CommodityID: 912067, Category: "cladorn", ResourceID: 187, Name: "中山装[30天]"},
		{CommodityID: 920043, Category: "item", ResourceID: 15, Name: "大体力药水"},
	}
	catalog, err := NewCatalog(848, items, commodities, DefaultCommodityLimit)
	if err != nil {
		t.Fatal(err)
	}
	product, ok := catalog.Lookup(912067)
	if !ok || product.ItemID != 2067 || product.Type != 0x0404 || product.SugarPrice != 1000 || catalog.Version() != 848 {
		t.Fatalf("product/catalog = %+v, found=%v, version=%d", product, ok, catalog.Version())
	}
	if len(catalog.Products()) != 2 {
		t.Fatalf("products = %d", len(catalog.Products()))
	}
}

func TestCommodityBundleQuantityMatchesPublishedINIAndSettlementCatalog(t *testing.T) {
	items := []itemcatalog.RegistryEntry{{ItemID: 20043, Category: "item", ResourceID: 20043, Name: "大体力药水"}}
	commodities := []itemcatalog.CommodityEntry{{CommodityID: 20043, Category: "item", ResourceID: 20043, Name: "大体力药水[100个"}}
	data, _, err := BuildCommodityINI(848, items, commodities)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), string(data))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decoded, "ITEM.1.NUM = 100") {
		t.Fatalf("published bundle quantity missing:\n%s", decoded)
	}
	catalog, err := NewCatalog(848, items, commodities, DefaultCommodityLimit)
	if err != nil {
		t.Fatal(err)
	}
	product, found := catalog.Lookup(20043)
	if !found || product.Quantity != 100 {
		t.Fatalf("bundle product = %+v, found=%t", product, found)
	}
	if got := commodityBundleQuantity("10格宠物栏"); got != 1 {
		t.Fatalf("ordinary numeric product name quantity = %d, want 1", got)
	}
}

func TestDefaultCommodityLimitKeepsCompleteRegistry(t *testing.T) {
	if DefaultCommodityLimit != 0 {
		t.Fatalf("default commodity limit = %d, want unlimited", DefaultCommodityLimit)
	}
	rows := make([]Row, 0, 1200)
	for index := 0; index < 400; index++ {
		for categoryOffset, category := range []string{"cap", "item", "petcard"} {
			rows = append(rows, Row{Commodity: itemcatalog.CommodityEntry{
				CommodityID: uint32(categoryOffset*10000 + index + 1),
				Category:    category,
			}})
		}
	}
	selected := selectEvenlyByCategory(rows, DefaultCommodityLimit)
	if len(selected) != len(rows) {
		t.Fatalf("selected rows = %d, want complete registry %d", len(selected), len(rows))
	}
}
