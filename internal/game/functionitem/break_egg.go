// Package functionitem owns server-side operations for the original shop's
// “功能道具” category. Client-rendered passive effects remain data driven;
// operations that debit or grant account inventory are authoritative here.
package functionitem

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
)

const (
	IronEggItemID   uint16 = 9001
	SilverEggItemID uint16 = 9002
	GoldEggItemID   uint16 = 9003
	ColorEggItemID  uint16 = 9004

	IronHammerItemID   uint16 = 9011
	SilverHammerItemID uint16 = 9012
	GoldHammerItemID   uint16 = 9013
)

type Reward struct {
	ItemID   uint16 `json:"item_id"`
	Quantity uint32 `json:"quantity"`
	Weight   uint32 `json:"weight"`
}

type BreakEggConfig struct {
	SchemaVersion int                 `json:"schema_version"`
	EggTiers      map[uint16]uint16   `json:"egg_tiers"`
	HammerBoost   map[uint16]uint16   `json:"hammer_boost"`
	RewardTiers   map[uint16][]Reward `json:"reward_tiers"`
}

type BreakEggCatalog struct {
	eggTiers    map[uint16]uint16
	hammerBoost map[uint16]uint16
	rewardTiers map[uint16][]Reward
	maximumTier uint16
}

func LoadBreakEggCatalog(path string) (*BreakEggCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open break-egg rewards: %w", err)
	}
	defer file.Close()
	var config BreakEggConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode break-egg rewards: %w", err)
	}
	if config.SchemaVersion != 1 {
		return nil, fmt.Errorf("break-egg reward schema %d, want 1", config.SchemaVersion)
	}
	catalog := &BreakEggCatalog{
		eggTiers: make(map[uint16]uint16, len(config.EggTiers)), hammerBoost: make(map[uint16]uint16, len(config.HammerBoost)),
		rewardTiers: make(map[uint16][]Reward, len(config.RewardTiers)),
	}
	for itemID, tier := range config.EggTiers {
		if !IsEgg(itemID) || tier == 0 {
			return nil, fmt.Errorf("break-egg egg %d has invalid tier %d", itemID, tier)
		}
		catalog.eggTiers[itemID] = tier
		if tier > catalog.maximumTier {
			catalog.maximumTier = tier
		}
	}
	for itemID, boost := range config.HammerBoost {
		if !IsHammer(itemID) {
			return nil, fmt.Errorf("break-egg hammer %d is not supported", itemID)
		}
		catalog.hammerBoost[itemID] = boost
	}
	for tier, rewards := range config.RewardTiers {
		if tier == 0 || len(rewards) == 0 {
			return nil, fmt.Errorf("break-egg reward tier %d is empty", tier)
		}
		copied := append([]Reward(nil), rewards...)
		var total uint64
		for index, reward := range copied {
			if reward.ItemID == 0 || reward.Quantity == 0 || reward.Weight == 0 {
				return nil, fmt.Errorf("break-egg reward tier %d entry %d is incomplete", tier, index)
			}
			total += uint64(reward.Weight)
		}
		if total > uint64(^uint32(0)) {
			return nil, fmt.Errorf("break-egg reward tier %d weight total %d exceeds uint32", tier, total)
		}
		catalog.rewardTiers[tier] = copied
	}
	if len(catalog.eggTiers) != 4 || len(catalog.hammerBoost) != 3 || catalog.maximumTier == 0 {
		return nil, fmt.Errorf("break-egg catalog requires four eggs and three hammers")
	}
	for tier := uint16(1); tier <= catalog.maximumTier; tier++ {
		if len(catalog.rewardTiers[tier]) == 0 {
			return nil, fmt.Errorf("break-egg reward tier %d is missing", tier)
		}
	}
	return catalog, nil
}

func IsEgg(itemID uint16) bool {
	return itemID >= IronEggItemID && itemID <= ColorEggItemID
}

func IsHammer(itemID uint16) bool {
	return itemID >= IronHammerItemID && itemID <= GoldHammerItemID
}

func (catalog *BreakEggCatalog) Select(eggID, hammerID uint16, entropy io.Reader) (Reward, uint16, error) {
	if catalog == nil {
		return Reward{}, 0, fmt.Errorf("break-egg reward catalog is unavailable")
	}
	baseTier, ok := catalog.eggTiers[eggID]
	if !ok {
		return Reward{}, 0, fmt.Errorf("item %d is not a configured egg", eggID)
	}
	boost, ok := catalog.hammerBoost[hammerID]
	if !ok {
		return Reward{}, 0, fmt.Errorf("item %d is not a configured hammer", hammerID)
	}
	tier := baseTier + boost
	if tier > catalog.maximumTier {
		tier = catalog.maximumTier
	}
	rewards := catalog.rewardTiers[tier]
	var total int64
	for _, reward := range rewards {
		total += int64(reward.Weight)
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	draw, err := rand.Int(entropy, big.NewInt(total))
	if err != nil {
		return Reward{}, 0, fmt.Errorf("draw break-egg reward tier %d: %w", tier, err)
	}
	selected := uint64(draw.Int64())
	for _, reward := range rewards {
		if selected < uint64(reward.Weight) {
			return reward, tier, nil
		}
		selected -= uint64(reward.Weight)
	}
	return Reward{}, 0, fmt.Errorf("break-egg reward tier %d selection overflow", tier)
}

func (catalog *BreakEggCatalog) Rewards() []Reward {
	if catalog == nil {
		return nil
	}
	var result []Reward
	for _, rewards := range catalog.rewardTiers {
		result = append(result, rewards...)
	}
	return result
}
