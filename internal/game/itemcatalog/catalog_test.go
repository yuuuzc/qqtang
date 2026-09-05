package itemcatalog

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestLoadGB2312CatalogAndMergeNewerNames(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	writeGBK := func(name, body string) {
		t.Helper()
		encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config, name), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeGBK("propdescrip.xml", `<?xml version="1.0" encoding="gb2312"?><propdescription><材料><mm id="30001" name="旧瓶子"/></材料></propdescription>`)
	writeGBK("DealProp2.xml", `<?xml version="1.0" encoding="gb2312"?><propdescription><type id="3" name="材料"><prop id="30001" name="透明的瓶子" description="测试"/></type></propdescription>`)
	entries, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != 30001 || entries[0].Name != "透明的瓶子" || entries[0].Kind != "material" || !entries[0].ClientSceneFactorySupported {
		t.Fatalf("catalog = %+v", entries)
	}
	if len(entries[0].SourceFiles) != 2 || entries[0].Description != "测试" {
		t.Fatalf("merged entry = %+v", entries[0])
	}
}

func TestLoadItemCFGIncludesPetFoodCardsAndDecorations(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	object := filepath.Join(root, "object")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(object, 0o700); err != nil {
		t.Fatal(err)
	}
	writeGBK := func(path, body string) {
		t.Helper()
		encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeGBK(filepath.Join(config, "propdescrip.xml"), `<?xml version="1.0" encoding="gb2312"?><root/>`)
	writeGBK(filepath.Join(config, "DealProp2.xml"), `<?xml version="1.0" encoding="gb2312"?><root/>`)
	writeGBK(filepath.Join(object, "itemCFG.py"), `version = 848
itemList = {
  1:('bg', 1, '秋天的气息', '', '', 0),
  26002:('food', 2, '宠物粮食小', '', '', 0),
  28001:('petcard', 1, '普通的酷比', '', '', 0),
}`)
	entries, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := map[uint32]string{1: "profile-decoration", 26002: "pet-food", 28001: "pet-card"}
	if len(entries) != len(wantKinds) {
		t.Fatalf("itemCFG catalog count = %d, want %d", len(entries), len(wantKinds))
	}
	for _, entry := range entries {
		if entry.Kind != wantKinds[entry.ID] {
			t.Fatalf("itemCFG entry = %+v", entry)
		}
		if entry.Index == 0 {
			t.Fatalf("itemCFG entry index was not preserved: %+v", entry)
		}
		if entry.RegistryCategory == "" {
			t.Fatalf("itemCFG registry category was not preserved: %+v", entry)
		}
	}
}

func TestLoadRegistriesKeepItemAndCommodityIdentitiesSeparate(t *testing.T) {
	root := t.TempDir()
	object := filepath.Join(root, "object")
	if err := os.Mkdir(object, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(object, name), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("itemCFG.py", `version = 848
itemList = {
  4696:('cladorn', 551, '蓝色忍者衫', '角色装扮', '', 0),
}`)
	write("commodityCFG.py", `version = 848
commodityList = {
  90001:('cladorn', 551, '蓝色忍者衫商品', '商城入口'),
	90001:('cladorn', 551, '蓝色忍者衫商品（最终）', '商城最终入口'),
}`)
	itemVersion, items, err := LoadItemRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	commodityVersion, commodities, err := LoadCommodityRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if itemVersion != 848 || commodityVersion != 848 || len(items) != 1 || len(commodities) != 1 {
		t.Fatalf("versions/items/commodities = %d/%d %+v %+v", itemVersion, commodityVersion, items, commodities)
	}
	if items[0].ItemID != 4696 || items[0].ResourceID != 551 || items[0].Description != "角色装扮" {
		t.Fatalf("item registry entry = %+v", items[0])
	}
	if commodities[0].CommodityID != 90001 || commodities[0].ResourceID != 551 || commodities[0].Name != "蓝色忍者衫商品（最终）" || commodities[0].Description != "商城最终入口" {
		t.Fatalf("commodity registry entry = %+v", commodities[0])
	}
}
