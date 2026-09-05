package functionitem

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBreakEggCatalogAppliesHammerTierBoost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewards.json")
	data := `{"schema_version":1,"egg_tiers":{"9001":1,"9002":2,"9003":3,"9004":4},"hammer_boost":{"9011":0,"9012":1,"9013":2},"reward_tiers":{"1":[{"item_id":10,"quantity":1,"weight":1}],"2":[{"item_id":20,"quantity":1,"weight":1}],"3":[{"item_id":30,"quantity":1,"weight":1}],"4":[{"item_id":40,"quantity":1,"weight":1}]}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadBreakEggCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	reward, tier, err := catalog.Select(IronEggItemID, GoldHammerItemID, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if tier != 3 || reward.ItemID != 30 {
		t.Fatalf("tier/reward = %d/%+v", tier, reward)
	}
}
