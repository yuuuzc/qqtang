package probe

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"math"
	"strings"

	"qqtang/internal/game/itemeffect"
	"qqtang/internal/protocol/game"
)

const antiKickCardItemID = uint16(100)

type competitivePointEffects struct {
	BasePoints       uint32
	EquipmentPercent uint32
	WinMultiplier    uint32
	PetPercent       uint32
	PetDoubleChance  uint32
	PetDoubleApplied bool
	FinalPoints      uint32
}

const vipCardItemID uint16 = 121

func profileOwnsActiveItem(profile game.PlayerProfile, itemID uint16) bool {
	for _, item := range profile.Inventory {
		if item.ItemID == itemID && item.Active() {
			return true
		}
	}
	return false
}

func profileOwnsActiveUncollectedItem(profile game.PlayerProfile, itemID uint16) bool {
	for _, item := range profile.Inventory {
		if item.ItemID == itemID && item.Active() && item.ItemStatus != game.ItemStatusCollected {
			return true
		}
	}
	return false
}

type petRewardEffects struct {
	BaseValue     uint32
	Percent       uint32
	DoubleChance  uint32
	DoubleApplied bool
	FinalValue    uint32
}

func (server *Server) profileHasKickProtection(profile game.PlayerProfile) bool {
	if game.PlayerIdentity(profile.Identity)&game.IdentityPurpleDiamond != 0 {
		return true
	}
	for _, item := range profile.Inventory {
		if item.ItemID == antiKickCardItemID && item.Active() {
			return true
		}
	}
	return false
}

// applyCompetitiveItemPointEffects evaluates only deterministic account and
// equipped-item promises. Pet talents/skills are intentionally a separate
// phase because they require resolving the currently carried pet and seeded
// chance rolls. Unknown catalog rules never affect progression.
func (server *Server) applyCompetitiveItemPointEffects(profile game.PlayerProfile, roleID byte, result game.GameResultCode, base uint32) competitivePointEffects {
	effects := competitivePointEffects{BasePoints: base, WinMultiplier: 1, FinalPoints: base}
	if server == nil || server.itemEffectCatalog == nil || base == 0 {
		return effects
	}
	for _, item := range profile.Inventory {
		if !item.Active() {
			continue
		}
		definition, ok := server.itemEffectCatalog.Lookup(uint32(item.ItemID))
		if !ok {
			continue
		}
		for _, rule := range definition.Rules {
			if rule.Trigger != itemeffect.TriggerCompetitiveSettlement {
				continue
			}
			switch rule.Kind {
			case itemeffect.KindPointsPercent:
				if definition.Source != itemeffect.SourceEquipment || item.ItemStatus == 0 || item.ItemRoleID != roleID {
					continue
				}
				effects.EquipmentPercent += uint32(rule.Percent)
			case itemeffect.KindPointsMultiplier:
				if result == game.GameResultWin && uint32(rule.Multiplier) > effects.WinMultiplier {
					effects.WinMultiplier = uint32(rule.Multiplier)
				}
			}
		}
	}
	return finalizeCompetitivePointEffects(effects)
}

func finalizeCompetitivePointEffects(effects competitivePointEffects) competitivePointEffects {
	points := uint64(effects.BasePoints)
	points += uint64(effects.BasePoints) * uint64(effects.EquipmentPercent+effects.PetPercent) / 100
	points *= uint64(effects.WinMultiplier)
	if effects.PetDoubleApplied {
		points *= 2
	}
	if points > math.MaxUint32 {
		points = math.MaxUint32
	}
	effects.FinalPoints = uint32(points)
	return effects
}

func (server *Server) applyCompetitiveSessionRewardEffects(session *connectionSession, result game.GameResultCode, basePoints, baseMoney uint32) (competitivePointEffects, petRewardEffects, error) {
	effects := server.applyCompetitiveItemPointEffects(session.Profile, session.selectedRoleID(), result, basePoints)
	moneyEffects := petRewardEffects{BaseValue: baseMoney, FinalValue: baseMoney}
	itemIDs, petID, err := server.carriedPetEffectItemIDs(session)
	if err != nil {
		return competitivePointEffects{}, petRewardEffects{}, err
	}
	if petID == 0 {
		return effects, moneyEffects, nil
	}
	effects.PetPercent, effects.PetDoubleChance = server.petRewardBonuses(itemIDs, "积分")
	effects.PetDoubleApplied = petRewardChanceRoll(session.CurrentGameID, session.UIN, petID, basePoints, effects.PetDoubleChance, "积分", 0)
	moneyEffects = server.applyPetRewardEffects(session, itemIDs, petID, "糖币", baseMoney, 0)
	return finalizeCompetitivePointEffects(effects), moneyEffects, nil
}

func (server *Server) carriedPetEffectItemIDs(session *connectionSession) ([]uint16, uint32, error) {
	if session == nil || session.Profile.GameInfo.PetID == 0 || server.playerStore == nil || server.itemEffectCatalog == nil {
		return nil, 0, nil
	}
	petID := session.Profile.GameInfo.PetID
	players, err := server.players()
	if err != nil {
		return nil, 0, err
	}
	pets, err := players.ListPets(context.Background(), session.UIN)
	if err != nil {
		return nil, 0, err
	}
	var carried *game.PetInfo
	for index := range pets {
		if pets[index].PetID == petID && pets[index].PetState == game.PetStateActive {
			carried = &pets[index]
			break
		}
	}
	if carried == nil {
		return nil, 0, nil
	}
	itemIDs := make([]uint16, 0, 1+len(carried.Skills))
	if itemID := server.petCardItemByType[carried.PetTypeID]; itemID != 0 {
		itemIDs = append(itemIDs, itemID)
	}
	for _, skillID := range carried.Skills {
		if itemID := server.petSkillItemByID[skillID]; itemID != 0 {
			itemIDs = append(itemIDs, itemID)
		}
	}
	return itemIDs, carried.PetID, nil
}

func (server *Server) competitivePetPointBonuses(itemIDs []uint16) (percent, doubleChance uint32) {
	return server.petRewardBonuses(itemIDs, "积分")
}

func (server *Server) petRewardBonuses(itemIDs []uint16, target string) (percent, doubleChance uint32) {
	for _, itemID := range itemIDs {
		definition, ok := server.itemEffectCatalog.Lookup(uint32(itemID))
		if !ok {
			continue
		}
		for _, rule := range definition.Rules {
			prefix := target + ":"
			if rule.Kind != itemeffect.KindPetTalent || !strings.HasPrefix(rule.Target, prefix) {
				continue
			}
			switch strings.TrimPrefix(rule.Target, prefix) {
			case "deterministic_percent":
				percent += uint32(rule.Percent)
			case "chance_double":
				doubleChance += uint32(rule.Chance)
			}
		}
	}
	if doubleChance > 100 {
		doubleChance = 100
	}
	return percent, doubleChance
}

func (server *Server) applyPetRewardEffects(session *connectionSession, itemIDs []uint16, petID uint32, target string, base, salt uint32) petRewardEffects {
	effects := petRewardEffects{BaseValue: base, FinalValue: base}
	if base == 0 || petID == 0 {
		return effects
	}
	effects.Percent, effects.DoubleChance = server.petRewardBonuses(itemIDs, target)
	effects.DoubleApplied = petRewardChanceRoll(session.CurrentGameID, session.UIN, petID, base, effects.DoubleChance, target, salt)
	value := uint64(base) + uint64(base)*uint64(effects.Percent)/100
	if effects.DoubleApplied {
		value *= 2
	}
	if value > math.MaxUint32 {
		value = math.MaxUint32
	}
	effects.FinalValue = uint32(value)
	return effects
}

func petRewardChanceRoll(gameID, uin, petID, value, chance uint32, target string, salt uint32) bool {
	if chance == 0 {
		return false
	}
	if chance >= 100 {
		return true
	}
	var input [24]byte
	binary.BigEndian.PutUint32(input[0:4], gameID)
	binary.BigEndian.PutUint32(input[4:8], uin)
	binary.BigEndian.PutUint32(input[8:12], petID)
	binary.BigEndian.PutUint32(input[12:16], value)
	binary.BigEndian.PutUint32(input[16:20], chance)
	binary.BigEndian.PutUint32(input[20:24], salt)
	hash := fnv.New32a()
	_, _ = hash.Write(input[:])
	_, _ = hash.Write([]byte(target))
	return hash.Sum32()%100 < chance
}
