// Package craftcatalog owns the original client-side synthesis and avatar
// forge definitions. Keeping these rules outside the protocol adapter makes
// item validation and persistence independently testable.
package craftcatalog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
)

// ForgeOperation values come from uiShop.pyc's g_Operator tuple (1, 3, 4).
// Value 2 is intentionally not assigned: it is not one of the three avatar
// forge actions exposed by this client.
type ForgeOperation byte

const (
	ForgeApply  ForgeOperation = 1
	ForgeRevert ForgeOperation = 3
	ForgeSplit  ForgeOperation = 4
)

func (operation ForgeOperation) Valid() bool {
	return operation == ForgeApply || operation == ForgeRevert || operation == ForgeSplit
}

type ForgeClass string

const (
	ForgeCap  ForgeClass = "cap"
	ForgeBody ForgeClass = "body"
	ForgeWing ForgeClass = "wing"
	ForgeBomb ForgeClass = "bomb"
)

// ForgeColor is the index consumed by the client's AvatarForge_wear path.
// The original config uses red for index 3 even though some item descriptions
// call the same visual family orange.
type ForgeColor byte

const (
	ForgeColorWhite ForgeColor = iota
	ForgeColorBlack
	ForgeColorBlue
	ForgeColorRed
	ForgeColorGreen
	ForgeColorPurple
)

// ForgeApplySuccessPercent is the restoration rule used while the original
// per-crystal probability table remains unavailable. Split and revert are
// deterministic maintenance operations and do not use this roll.
const ForgeApplySuccessPercent = 50

type ForgeItem struct {
	ItemID uint16
	Class  ForgeClass
}

type EffectForm struct {
	ID        byte
	Class     ForgeClass
	Level     byte
	Name      string
	Direction byte
}

type MaterialRule struct {
	ItemID          uint16
	PreferredLevels []byte
	PreferredColors []ForgeColor
	// ExactProbabilityKnown remains false until an authoritative probability
	// table is recovered. Preferred candidates are confirmed by itemCFG text.
	ExactProbabilityKnown bool
}

type MaterialCost struct {
	ItemID   uint16
	Quantity uint32
}

type Catalog struct {
	Version             uint32
	ApplySuccessPercent int
	Items               map[uint16]ForgeItem
	Effects             map[byte]EffectForm
	Colors              map[ForgeColor]string
	Materials           map[uint16]MaterialRule
	Split               MaterialCost
	Revert              MaterialCost
}

type ForgePlan struct {
	Operation        ForgeOperation
	Succeeded        bool
	TargetItemID     uint16
	MaterialItemID   uint16
	MaterialQuantity uint32
	PreviousEffect   byte
	PreviousColor    byte
	Effect           byte
	Color            byte
}

// LoadForge reads config/avatarforge.ini from the original client tree.
func LoadForge(clientRoot string) (*Catalog, error) {
	if strings.TrimSpace(clientRoot) == "" {
		return nil, fmt.Errorf("avatar forge client root is empty")
	}
	_, entries, err := itemcatalog.LoadItemRegistry(clientRoot)
	if err != nil {
		return nil, fmt.Errorf("load avatar forge material descriptions: %w", err)
	}
	materialRules, err := forgeMaterialRulesFromItemCFG(entries)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(clientRoot, "config", "avatarforge.ini"))
	if err != nil {
		return nil, fmt.Errorf("open avatar forge config: %w", err)
	}
	defer file.Close()
	return parseForge(file, materialRules)
}

func parseForge(reader io.Reader, materialRules map[uint16]MaterialRule) (*Catalog, error) {
	catalog := &Catalog{
		ApplySuccessPercent: ForgeApplySuccessPercent,
		Items:               make(map[uint16]ForgeItem), Effects: make(map[byte]EffectForm),
		Colors: make(map[ForgeColor]string), Materials: make(map[uint16]MaterialRule),
	}
	sections := make(map[string]map[string]string)
	section := ""
	scanner := bufio.NewScanner(reader)
	// The shipped 2008 INI uses bare carriage returns rather than CRLF.
	// bufio.ScanLines does not split that legacy form.
	scanner.Split(scanLegacyLines)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if sections[section] == nil {
				sections[section] = make(map[string]string)
			}
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || section == "" {
			continue
		}
		sections[section][strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read avatar forge config: %w", err)
	}
	version, err := parseUint(sections["public"]["version"], 32, "public.version")
	if err != nil {
		return nil, err
	}
	catalog.Version = uint32(version)

	forgeTypeCount, err := parseUint(sections["forgetype"]["count"], 8, "forgetype.count")
	if err != nil {
		return nil, err
	}
	for index := uint64(1); index <= forgeTypeCount; index++ {
		name := fmt.Sprintf("forgetype.%d", index)
		values := sections[name]
		class := ForgeClass(strings.ToLower(values["typename"]))
		if class != ForgeCap && class != ForgeBody && class != ForgeWing && class != ForgeBomb {
			return nil, fmt.Errorf("%s has unsupported typename %q", name, class)
		}
		lineCount, parseErr := parseUint(values["count"], 16, name+".count")
		if parseErr != nil {
			return nil, parseErr
		}
		for lineIndex := uint64(1); lineIndex <= lineCount; lineIndex++ {
			for _, token := range strings.Fields(values[fmt.Sprintf("item.%d", lineIndex)]) {
				itemID, parseErr := parseUint(token, 16, name+" item")
				if parseErr != nil {
					return nil, parseErr
				}
				id := uint16(itemID)
				if existing, found := catalog.Items[id]; found && existing.Class != class {
					return nil, fmt.Errorf("forge item %d belongs to both %s and %s", id, existing.Class, class)
				}
				catalog.Items[id] = ForgeItem{ItemID: id, Class: class}
			}
		}
	}

	formCount, err := parseUint(sections["effectform"]["formcount"], 8, "effectform.formcount")
	if err != nil {
		return nil, err
	}
	for index := uint64(1); index <= formCount; index++ {
		key := fmt.Sprintf("form.%d", index)
		name := strings.ToLower(sections["effectform"][key])
		class, level, parseErr := parseEffectName(name)
		if parseErr != nil {
			return nil, parseErr
		}
		direction, _ := strconv.ParseUint(sections["effectform"][key+".direction"], 10, 8)
		catalog.Effects[byte(index)] = EffectForm{ID: byte(index), Class: class, Level: level, Name: name, Direction: byte(direction)}
	}

	colorCount, err := parseUint(sections["colorlist.1"]["colorcount"], 8, "colorlist.1.colorcount")
	if err != nil {
		return nil, err
	}
	for index := uint64(0); index < colorCount; index++ {
		catalog.Colors[ForgeColor(index)] = strings.ToLower(sections["colorlist.1"][fmt.Sprintf("color.%d", index)])
	}

	materialIDs := strings.Fields(sections["material"]["item.1"])
	for _, token := range materialIDs {
		itemID, parseErr := parseUint(token, 16, "material item")
		if parseErr != nil {
			return nil, parseErr
		}
		rule, found := materialRules[uint16(itemID)]
		if !found {
			return nil, fmt.Errorf("avatar forge material %d has no itemCFG-derived candidate rule", itemID)
		}
		catalog.Materials[rule.ItemID] = rule
	}
	if catalog.Split, err = parseCost(sections["split"], "split"); err != nil {
		return nil, err
	}
	if catalog.Revert, err = parseCost(sections["revert"], "revert"); err != nil {
		return nil, err
	}
	if len(catalog.Items) == 0 || len(catalog.Effects) != 20 || len(catalog.Colors) != 6 || len(catalog.Materials) != 7 {
		return nil, fmt.Errorf("avatar forge config is incomplete: items=%d effects=%d colors=%d materials=%d", len(catalog.Items), len(catalog.Effects), len(catalog.Colors), len(catalog.Materials))
	}
	return catalog, nil
}

func (catalog *Catalog) SetApplySuccessPercent(percent int) error {
	if catalog == nil {
		return fmt.Errorf("avatar forge catalog is nil")
	}
	if percent < 0 || percent > 100 {
		return fmt.Errorf("avatar forge success percent %d is outside 0..100", percent)
	}
	catalog.ApplySuccessPercent = percent
	return nil
}

func scanLegacyLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for index, value := range data {
		if value != '\r' && value != '\n' {
			continue
		}
		advance = index + 1
		if value == '\r' && advance < len(data) && data[advance] == '\n' {
			advance++
		}
		return advance, data[:index], nil
	}
	if atEOF && len(data) != 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

var forgeLevelPattern = regexp.MustCompile(`([1-5])[级极]`)

var forgeDescriptionColors = []struct {
	name  string
	color ForgeColor
}{
	// avatarforge.ini has no orange entry. The original client's orange
	// wording is rendered by color index 3, whose configured name is red.
	{name: "橙色", color: ForgeColorRed},
	{name: "蓝色", color: ForgeColorBlue},
	{name: "绿色", color: ForgeColorGreen},
	{name: "紫色", color: ForgeColorPurple},
}

// forgeMaterialRulesFromItemCFG makes itemCFG.py's installed descriptions the
// source of truth for the seven crystals. The text explicitly lists each
// possible effect level and color; candidates are deliberately not inferred
// from the crystal's own display name.
func forgeMaterialRulesFromItemCFG(entries []itemcatalog.RegistryEntry) (map[uint16]MaterialRule, error) {
	const firstForgeCrystal = 20051
	const lastForgeCrystal = 20057
	rules := make(map[uint16]MaterialRule, lastForgeCrystal-firstForgeCrystal+1)
	for _, entry := range entries {
		if entry.ItemID < firstForgeCrystal || entry.ItemID > lastForgeCrystal {
			continue
		}
		rule := MaterialRule{ItemID: uint16(entry.ItemID)}
		seenLevel := make(map[byte]bool)
		for _, match := range forgeLevelPattern.FindAllStringSubmatch(entry.Description, -1) {
			level := byte(match[1][0] - '0')
			if !seenLevel[level] {
				rule.PreferredLevels = append(rule.PreferredLevels, level)
				seenLevel[level] = true
			}
		}
		for _, candidate := range forgeDescriptionColors {
			if strings.Contains(entry.Description, candidate.name) {
				rule.PreferredColors = append(rule.PreferredColors, candidate.color)
			}
		}
		if len(rule.PreferredLevels) == 0 || len(rule.PreferredColors) == 0 {
			return nil, fmt.Errorf(
				"avatar forge material %d description %q has incomplete level/color candidates",
				entry.ItemID, entry.Description,
			)
		}
		rules[rule.ItemID] = rule
	}
	if len(rules) != lastForgeCrystal-firstForgeCrystal+1 {
		return nil, fmt.Errorf("avatar forge itemCFG material descriptions are incomplete: got %d, want 7", len(rules))
	}
	return rules, nil
}

func (catalog *Catalog) Plan(operation ForgeOperation, item game.ItemInfo, materialID uint16, entropy io.Reader) (ForgePlan, error) {
	if catalog == nil || !operation.Valid() || item.ItemID == 0 || !item.Active() {
		return ForgePlan{}, fmt.Errorf("invalid avatar forge request")
	}
	plan := ForgePlan{
		Operation: operation, TargetItemID: item.ItemID, MaterialItemID: materialID,
		PreviousEffect: item.ItemEffect, PreviousColor: item.ItemColor,
	}
	switch operation {
	case ForgeApply:
		definition, found := catalog.Items[item.ItemID]
		if !found {
			return ForgePlan{}, fmt.Errorf("item %d is not listed by avatarforge.ini", item.ItemID)
		}
		material, found := catalog.Materials[materialID]
		if !found {
			return ForgePlan{}, fmt.Errorf("item %d is not an avatar forge crystal", materialID)
		}
		plan.MaterialQuantity = 1
		plan.Effect = item.ItemEffect
		plan.Color = item.ItemColor
		succeeded, err := rollPercent(entropy, catalog.ApplySuccessPercent)
		if err != nil {
			return ForgePlan{}, err
		}
		if !succeeded {
			return plan, nil
		}
		currentLevel := byte(0)
		if item.ItemEffect != 0 {
			current, ok := catalog.Effects[item.ItemEffect]
			if !ok || current.Class != definition.Class {
				return ForgePlan{}, fmt.Errorf("item %d has incompatible forge effect %d", item.ItemID, item.ItemEffect)
			}
			currentLevel = current.Level
		}
		levelIndex, err := chooseIndex(entropy, len(material.PreferredLevels))
		if err != nil {
			return ForgePlan{}, err
		}
		colorIndex, err := chooseIndex(entropy, len(material.PreferredColors))
		if err != nil {
			return ForgePlan{}, err
		}
		selectedLevel := material.PreferredLevels[levelIndex]
		if currentLevel == 0 || selectedLevel > currentLevel {
			form, found := catalog.effectFor(definition.Class, material.PreferredLevels[levelIndex])
			if !found {
				return ForgePlan{}, fmt.Errorf("no %s level %d effect form", definition.Class, material.PreferredLevels[levelIndex])
			}
			plan.Effect = form.ID
		}
		plan.Succeeded = true
		plan.Color = byte(material.PreferredColors[colorIndex])
	case ForgeSplit:
		plan.Succeeded = true
		if materialID != catalog.Split.ItemID {
			return ForgePlan{}, fmt.Errorf("split material %d, want %d", materialID, catalog.Split.ItemID)
		}
		if item.ItemColor == 0 {
			return ForgePlan{}, fmt.Errorf("item %d has no forge color to split", item.ItemID)
		}
		plan.MaterialQuantity = catalog.Split.Quantity
		plan.Effect = item.ItemEffect
		plan.Color = 0
	case ForgeRevert:
		plan.Succeeded = true
		if materialID != catalog.Revert.ItemID {
			return ForgePlan{}, fmt.Errorf("revert material %d, want %d", materialID, catalog.Revert.ItemID)
		}
		if item.ItemEffect == 0 && item.ItemColor == 0 {
			return ForgePlan{}, fmt.Errorf("item %d has no forge color or effect to revert", item.ItemID)
		}
		plan.MaterialQuantity = catalog.Revert.Quantity
		plan.Effect = 0
		plan.Color = 0
	}
	return plan, nil
}

func rollPercent(entropy io.Reader, percent int) (bool, error) {
	if entropy == nil {
		return false, fmt.Errorf("avatar forge entropy reader is nil")
	}
	if percent < 0 || percent > 100 {
		return false, fmt.Errorf("avatar forge success percent %d is outside 0..100", percent)
	}
	if percent == 0 || percent == 100 {
		return percent == 100, nil
	}
	// Discard the high tail so mapping a random byte to 0..99 is unbiased.
	const unbiasedLimit = 200
	for {
		var value [1]byte
		if _, err := io.ReadFull(entropy, value[:]); err != nil {
			return false, fmt.Errorf("read avatar forge probability entropy: %w", err)
		}
		if int(value[0]) >= unbiasedLimit {
			continue
		}
		return int(value[0])%100 < percent, nil
	}
}

func (catalog *Catalog) effectFor(class ForgeClass, level byte) (EffectForm, bool) {
	for _, form := range catalog.Effects {
		if form.Class == class && form.Level == level {
			return form, true
		}
	}
	return EffectForm{}, false
}

func chooseIndex(entropy io.Reader, count int) (int, error) {
	if entropy == nil {
		return 0, fmt.Errorf("avatar forge entropy reader is nil")
	}
	if count <= 0 {
		return 0, fmt.Errorf("avatar forge candidate list is empty")
	}
	var value [1]byte
	if _, err := io.ReadFull(entropy, value[:]); err != nil {
		return 0, fmt.Errorf("read avatar forge entropy: %w", err)
	}
	return int(value[0]) % count, nil
}

func parseEffectName(name string) (ForgeClass, byte, error) {
	for _, class := range []ForgeClass{ForgeCap, ForgeBody, ForgeWing, ForgeBomb} {
		prefix := string(class)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		level, err := strconv.ParseUint(strings.TrimPrefix(name, prefix), 10, 8)
		if err != nil || level < 1 || level > 5 {
			return "", 0, fmt.Errorf("invalid avatar forge effect form %q", name)
		}
		return class, byte(level), nil
	}
	return "", 0, fmt.Errorf("invalid avatar forge effect form %q", name)
}

func parseCost(values map[string]string, name string) (MaterialCost, error) {
	itemID, err := parseUint(values["itemid"], 16, name+".itemid")
	if err != nil {
		return MaterialCost{}, err
	}
	quantity, err := parseUint(values["itemnum"], 32, name+".itemnum")
	if err != nil {
		return MaterialCost{}, err
	}
	return MaterialCost{ItemID: uint16(itemID), Quantity: uint32(quantity)}, nil
}

func parseUint(value string, bits int, field string) (uint64, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("parse avatar forge %s=%q: %w", field, value, err)
	}
	return parsed, nil
}
