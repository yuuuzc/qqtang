package petcatalog

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"

	"qqtang/internal/game/itemcatalog"
)

const (
	SourceClientConfig   = "client-config"
	SourceCommunityIndex = "community-model-index"
	SourceLocalExtension = "local-client-extension"

	ResourceComplete    = "complete"
	ResourceStarterOnly = "starter-only"
	ResourceMissing     = "missing"

	PetStaminaPotionItemID    uint16 = 26001
	PetFoodSmallItemID        uint16 = 26002
	PetFoodMediumItemID       uint16 = 26003
	PetFoodLargeItemID        uint16 = 26004
	PetMoodCandySmallItemID   uint16 = 26005
	PetMoodCandyMediumItemID  uint16 = 26006
	PetMoodCandyLargeItemID   uint16 = 26007
	PetPeanutChocolateItemID  uint16 = 26008
	PetPineNutChocolateItemID uint16 = 26009
)

var petSectionPattern = regexp.MustCompile(`(?i)^pet([0-9]+)$`)

var (
	petSkillKeyPattern   = regexp.MustCompile(`(?i)^skill([0-9]+)$`)
	petSkillLevelPattern = regexp.MustCompile(`([0-9]+)级$`)
	petFoodValuePatterns = map[string]*regexp.Regexp{
		"experience": regexp.MustCompile(`增加宠物成长值([0-9]+)点`),
		"loyalty":    regexp.MustCompile(`增加宠物体力([0-9]+)点`),
		"mood":       regexp.MustCompile(`增加宠物心情([0-9]+)点`),
	}
)

//go:embed pet_card_types.json
var petCardTypeMappingJSON []byte

// Definition is one server-side pet type from the original client's
// config/PetCfg.ini. PetTypeID identifies a pet species/variant; it is not an
// inventory item ID and must never be inserted into player_inventory.
type Definition struct {
	PetTypeID          uint32   `json:"pet_type_id"`
	Name               string   `json:"name"`
	Level1ResourceID   uint32   `json:"level_1_resource_id"`
	Level4ResourceID   uint32   `json:"level_4_resource_id"`
	Level7ResourceID   uint32   `json:"level_7_resource_id"`
	Source             string   `json:"source"`
	Assignable         bool     `json:"assignable"`
	ResourceStatus     string   `json:"resource_status"`
	InstalledResources []string `json:"installed_resources,omitempty"`
	MissingResources   []string `json:"missing_resources,omitempty"`
}

// ModelFamily records late model IDs visible in preserved client footage but
// absent from this version's PetCfg.ini. They are useful restoration evidence,
// but cannot be assigned until an exact PetTypeID mapping is recovered.
type ModelFamily struct {
	LocalPetTypeID     uint32   `json:"local_pet_type_id"`
	Name               string   `json:"name"`
	JuvenileResourceID uint32   `json:"juvenile_resource_id"`
	AdultResourceID    uint32   `json:"adult_resource_id,omitempty"`
	Source             string   `json:"source"`
	EvidenceURL        string   `json:"evidence_url"`
	Assignable         bool     `json:"assignable"`
	Reason             string   `json:"reason"`
	ResourceStatus     string   `json:"resource_status"`
	InstalledResources []string `json:"installed_resources,omitempty"`
	MissingResources   []string `json:"missing_resources,omitempty"`
}

// CardLink joins the account-inventory item identity from itemCFG with the
// independent pet entity identity from PetCfg. The original client uses both
// IDs for one user-facing pet, so the GM UI must not guess from either number.
type CardLink struct {
	ItemID      uint32 `json:"item_id"`
	ResourceID  uint32 `json:"resource_id"`
	PetTypeID   uint32 `json:"pet_type_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// InnateSkills is derived from the original pet-card description and
	// PetCfg's 1..48 talent table. It is wire projection metadata, not learned
	// account state, and must never be persisted in player_pets.skills.
	InnateSkills []byte `json:"innate_skills,omitempty"`
}

// SkillDefinition is one client-recognized PET_BASE_INFO.Skills byte. IDs
// 1..48 are innate talents; learned skill books map to the sparse IDs >= 51.
type SkillDefinition struct {
	SkillID     byte   `json:"skill_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Level       byte   `json:"level,omitempty"`
	Learned     bool   `json:"learned"`
}

// SkillBookLink joins the canonical inventory item with PetCfg's sparse skill
// ID. itemCFG's category-local resource ID is deliberately retained only as
// resource metadata; it is not the byte stored in PET_BASE_INFO.Skills.
type SkillBookLink struct {
	ItemID      uint32          `json:"item_id"`
	ResourceID  uint32          `json:"resource_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Skill       SkillDefinition `json:"skill"`
}

// FoodEffect uses the original message-table field names: PetLoyalty is the
// 0..1000 stamina bar rendered by the client, PetExperience drives levels,
// and PetMood is the 0..100 face/growth modifier.
type FoodEffect struct {
	Experience uint32 `json:"experience"`
	Loyalty    uint32 `json:"loyalty"`
	Mood       uint16 `json:"mood"`
}

type FoodLink struct {
	ItemID      uint32     `json:"item_id"`
	ResourceID  uint32     `json:"resource_id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Effect      FoodEffect `json:"effect"`
	Complete    bool       `json:"complete"`
}

// LearnedSkillLevel mirrors the sparse PetCfg layout. Learned families begin
// at 51 and repeat every three IDs; the two valid residues are interleaved
// skill families, while the third residue is unused.
func LearnedSkillLevel(skillID byte) (byte, bool) {
	if skillID < 51 {
		return 0, false
	}
	relative := int(skillID) - 51
	withinGroup := relative % 30
	if withinGroup%3 == 2 {
		return 0, false
	}
	level := byte(withinGroup/3 + 1)
	return level, level >= 1 && level <= 10
}

type Catalog struct {
	ConfigPath           string            `json:"config_path"`
	ExperienceThresholds []uint32          `json:"experience_thresholds"`
	Definitions          []Definition      `json:"definitions"`
	CardLinks            []CardLink        `json:"card_links"`
	Skills               []SkillDefinition `json:"skills"`
	SkillBooks           []SkillBookLink   `json:"skill_books"`
	Foods                []FoodLink        `json:"foods"`
	UnmappedModels       []ModelFamily     `json:"unmapped_models"`
}

// Load parses the original GBK PetCfg.ini and audits every referenced model
// against both loose files and data/object.pkg.
func Load(clientRoot string) (Catalog, error) {
	configPath := filepath.Join(clientRoot, "config", "PetCfg.ini")
	encoded, err := os.ReadFile(configPath)
	if err != nil {
		return Catalog{}, fmt.Errorf("read pet config: %w", err)
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(encoded)
	if err != nil {
		return Catalog{}, fmt.Errorf("decode pet config as GBK: %w", err)
	}
	definitions, experience, err := parse(string(decoded))
	if err != nil {
		return Catalog{}, fmt.Errorf("parse %s: %w", configPath, err)
	}
	skills, err := parseSkills(string(decoded))
	if err != nil {
		return Catalog{}, fmt.Errorf("parse pet skills from %s: %w", configPath, err)
	}
	cardLinks, err := loadCardLinks(clientRoot, definitions, skills)
	if err != nil {
		return Catalog{}, err
	}
	skillBooks, foods, err := loadConsumableLinks(clientRoot, skills)
	if err != nil {
		return Catalog{}, err
	}
	archive, archiveErr := itemcatalog.OpenObjectArchive(clientRoot)
	if archiveErr != nil && !errors.Is(archiveErr, os.ErrNotExist) {
		return Catalog{}, archiveErr
	}
	for index := range definitions {
		installed, missing := auditResourceIDs(clientRoot, archive,
			definitions[index].Level1ResourceID,
			definitions[index].Level4ResourceID,
			definitions[index].Level7ResourceID,
		)
		definitions[index].InstalledResources = installed
		definitions[index].MissingResources = missing
		definitions[index].ResourceStatus = resourceStatus(definitions[index].Level1ResourceID, installed, missing)
	}
	definitionByID := make(map[uint32]Definition, len(definitions))
	for _, definition := range definitions {
		definitionByID[definition.PetTypeID] = definition
	}
	allHidden := hiddenModelFamilies()
	unmapped := make([]ModelFamily, 0, len(allHidden))
	for _, model := range allHidden {
		if definition, installed := definitionByID[model.LocalPetTypeID]; installed && definition.Source == SourceLocalExtension {
			continue
		}
		unmapped = append(unmapped, model)
	}
	for index := range unmapped {
		installed, missing := auditResourceIDs(clientRoot, archive,
			unmapped[index].JuvenileResourceID, unmapped[index].AdultResourceID)
		unmapped[index].InstalledResources = installed
		unmapped[index].MissingResources = missing
		if len(missing) == 0 {
			unmapped[index].ResourceStatus = ResourceComplete
		} else {
			unmapped[index].ResourceStatus = ResourceMissing
		}
	}
	return Catalog{
		ConfigPath:           filepath.ToSlash(filepath.Join("config", "PetCfg.ini")),
		ExperienceThresholds: experience,
		Definitions:          definitions,
		CardLinks:            cardLinks,
		Skills:               skills,
		SkillBooks:           skillBooks,
		Foods:                foods,
		UnmappedModels:       unmapped,
	}, nil
}

func parseSkills(contents string) ([]SkillDefinition, error) {
	names := make(map[uint64]string)
	descriptions := make(map[uint64]string)
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if !strings.EqualFold(section, "SkillName") && !strings.EqualFold(section, "SkillDescription") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		match := petSkillKeyPattern.FindStringSubmatch(strings.TrimSpace(key))
		if match == nil {
			continue
		}
		id, err := strconv.ParseUint(match[1], 10, 8)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("invalid pet skill key %q", key)
		}
		if strings.EqualFold(section, "SkillName") {
			names[id] = strings.TrimSpace(value)
		} else {
			descriptions[id] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(names))
	for id := range names {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	result := make([]SkillDefinition, 0, len(ids))
	for _, rawID := range ids {
		id := uint64(rawID)
		definition := SkillDefinition{
			SkillID: byte(id), Name: names[id], Description: descriptions[id], Learned: id >= 51,
		}
		if definition.Learned {
			match := petSkillLevelPattern.FindStringSubmatch(definition.Name)
			if match == nil {
				return nil, fmt.Errorf("learned pet skill %d name %q has no level suffix", id, definition.Name)
			}
			level, err := strconv.ParseUint(match[1], 10, 8)
			if err != nil || level == 0 || level > 10 {
				return nil, fmt.Errorf("learned pet skill %d level %q is invalid", id, match[1])
			}
			definition.Level = byte(level)
		}
		result = append(result, definition)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no SkillName entries found")
	}
	return result, nil
}

func loadConsumableLinks(clientRoot string, skills []SkillDefinition) ([]SkillBookLink, []FoodLink, error) {
	_, registry, err := itemcatalog.LoadItemRegistry(clientRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("load item registry for pet consumables: %w", err)
	}
	skillByName := make(map[string]SkillDefinition)
	for _, skill := range skills {
		if skill.Learned {
			skillByName[skill.Name] = skill
		}
	}
	var books []SkillBookLink
	var foods []FoodLink
	for _, item := range registry {
		switch item.Category {
		case "skill":
			skill, found := skillByName[item.Name]
			if !found {
				return nil, nil, fmt.Errorf("pet skill book item %d name %q has no PetCfg skill", item.ItemID, item.Name)
			}
			books = append(books, SkillBookLink{
				ItemID: item.ItemID, ResourceID: item.ResourceID, Name: item.Name,
				Description: item.Description, Skill: skill,
			})
		case "food":
			effect, complete := parseFoodEffect(item.Description)
			if confirmed, found := confirmedFoodEffects[uint16(item.ItemID)]; found {
				effect, complete = confirmed, true
			}
			foods = append(foods, FoodLink{
				ItemID: item.ItemID, ResourceID: item.ResourceID, Name: item.Name,
				Description: item.Description, Effect: effect, Complete: complete,
			})
		}
	}
	sort.Slice(books, func(i, j int) bool { return books[i].ItemID < books[j].ItemID })
	sort.Slice(foods, func(i, j int) bool { return foods[i].ItemID < foods[j].ItemID })
	return books, foods, nil
}

// confirmedFoodEffects completes values omitted by two client descriptions
// and keeps the full original nine-food progression in one reviewable table.
var confirmedFoodEffects = map[uint16]FoodEffect{
	PetStaminaPotionItemID:    {Loyalty: 100},
	PetFoodSmallItemID:        {Loyalty: 400, Experience: 20},
	PetFoodMediumItemID:       {Loyalty: 600, Experience: 30},
	PetFoodLargeItemID:        {Loyalty: 1000, Experience: 60},
	PetMoodCandySmallItemID:   {Mood: 1},
	PetMoodCandyMediumItemID:  {Mood: 20},
	PetMoodCandyLargeItemID:   {Mood: 50},
	PetPeanutChocolateItemID:  {Experience: 90},
	PetPineNutChocolateItemID: {Experience: 180},
}

func parseFoodEffect(description string) (FoodEffect, bool) {
	var effect FoodEffect
	complete := true
	for field, pattern := range petFoodValuePatterns {
		if !strings.Contains(description, map[string]string{
			"experience": "成长值", "loyalty": "体力", "mood": "心情",
		}[field]) {
			continue
		}
		match := pattern.FindStringSubmatch(description)
		if match == nil {
			complete = false
			continue
		}
		value, err := strconv.ParseUint(match[1], 10, 32)
		if err != nil || value == 0 {
			complete = false
			continue
		}
		switch field {
		case "experience":
			effect.Experience = uint32(value)
		case "loyalty":
			effect.Loyalty = uint32(value)
		case "mood":
			if value > 65535 {
				complete = false
			} else {
				effect.Mood = uint16(value)
			}
		}
	}
	if effect == (FoodEffect{}) {
		complete = false
	}
	return effect, complete
}

type cardTypeMappingFile struct {
	SchemaVersion    uint32   `json:"schema_version"`
	NonEntityItemIDs []uint32 `json:"non_entity_item_ids"`
	Mappings         []struct {
		ItemID    uint32 `json:"item_id"`
		PetTypeID uint32 `json:"pet_type_id"`
	} `json:"mappings"`
}

func loadCardLinks(clientRoot string, definitions []Definition, skills []SkillDefinition) ([]CardLink, error) {
	var mapping cardTypeMappingFile
	if err := json.Unmarshal(petCardTypeMappingJSON, &mapping); err != nil {
		return nil, fmt.Errorf("decode embedded pet-card mapping: %w", err)
	}
	if mapping.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported pet-card mapping schema %d", mapping.SchemaVersion)
	}
	_, registry, err := itemcatalog.LoadItemRegistry(clientRoot)
	if err != nil {
		return nil, fmt.Errorf("load item registry for pet-card mapping: %w", err)
	}
	registryByID := make(map[uint32]itemcatalog.RegistryEntry, len(registry))
	canonicalCardIDs := make(map[uint32]struct{})
	ignored := make(map[uint32]struct{}, len(mapping.NonEntityItemIDs))
	for _, itemID := range mapping.NonEntityItemIDs {
		ignored[itemID] = struct{}{}
	}
	for _, item := range registry {
		registryByID[item.ItemID] = item
		if item.Category == "petcard" {
			if _, skip := ignored[item.ItemID]; !skip {
				canonicalCardIDs[item.ItemID] = struct{}{}
			}
		}
	}
	definitionByID := make(map[uint32]Definition, len(definitions))
	for _, definition := range definitions {
		definitionByID[definition.PetTypeID] = definition
	}
	seenItems := make(map[uint32]struct{}, len(mapping.Mappings))
	seenTypes := make(map[uint32]struct{}, len(mapping.Mappings))
	links := make([]CardLink, 0, len(mapping.Mappings))
	for _, entry := range mapping.Mappings {
		if _, duplicate := seenItems[entry.ItemID]; duplicate {
			return nil, fmt.Errorf("duplicate pet-card item mapping %d", entry.ItemID)
		}
		if _, duplicate := seenTypes[entry.PetTypeID]; duplicate {
			return nil, fmt.Errorf("duplicate pet type mapping %d", entry.PetTypeID)
		}
		item, ok := registryByID[entry.ItemID]
		if !ok || item.Category != "petcard" {
			return nil, fmt.Errorf("pet-card mapping item %d is absent or not category petcard", entry.ItemID)
		}
		if _, ok := definitionByID[entry.PetTypeID]; !ok {
			return nil, fmt.Errorf("pet-card mapping type %d is absent from PetCfg.ini", entry.PetTypeID)
		}
		seenItems[entry.ItemID] = struct{}{}
		seenTypes[entry.PetTypeID] = struct{}{}
		innateSkills, err := deriveInnateSkills(item.Description, skills)
		if err != nil {
			return nil, fmt.Errorf("pet-card item %d (%s): %w", entry.ItemID, item.Name, err)
		}
		links = append(links, CardLink{
			ItemID: item.ItemID, ResourceID: item.ResourceID, PetTypeID: entry.PetTypeID,
			Name: item.Name, Description: item.Description, InnateSkills: innateSkills,
		})
	}
	for itemID := range canonicalCardIDs {
		if _, mapped := seenItems[itemID]; !mapped {
			return nil, fmt.Errorf("canonical pet-card item %d has no PetTypeID mapping", itemID)
		}
	}
	if len(seenItems) != len(canonicalCardIDs) {
		return nil, fmt.Errorf("pet-card mapping count %d does not match canonical card count %d", len(seenItems), len(canonicalCardIDs))
	}
	sort.Slice(links, func(i, j int) bool { return links[i].ItemID < links[j].ItemID })
	return links, nil
}

func deriveInnateSkills(description string, skills []SkillDefinition) ([]byte, error) {
	const marker = "具有天赋："
	start := strings.Index(description, marker)
	if start < 0 {
		return nil, nil
	}
	text := description[start+len(marker):]
	if end := strings.IndexAny(text, "。；;"); end >= 0 {
		text = text[:end]
	}
	byName := make(map[string]byte)
	for _, skill := range skills {
		if skill.Learned || skill.SkillID == 0 || skill.SkillID > 48 {
			continue
		}
		// Several historical pets intentionally share the same displayed
		// talent. The smallest original ID is the stable canonical wire value;
		// the displayed name and settlement semantics are identical.
		name := strings.TrimSpace(skill.Name)
		if previous, found := byName[name]; !found || skill.SkillID < previous {
			byName[name] = skill.SkillID
		}
	}
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == '，' || r == ',' || r == '、' })
	result := make([]byte, 0, len(parts))
	seen := make(map[byte]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		skillID, found := byName[name]
		if !found {
			return nil, fmt.Errorf("description talent %q is absent from PetCfg SkillName", name)
		}
		if _, duplicate := seen[skillID]; duplicate {
			continue
		}
		seen[skillID] = struct{}{}
		result = append(result, skillID)
	}
	if len(result) > 3 {
		return nil, fmt.Errorf("description defines %d innate talents, maximum is 3", len(result))
	}
	return result, nil
}

func parse(contents string) ([]Definition, []uint32, error) {
	type section struct {
		name    string
		comment string
		values  map[string]string
	}
	var sections []section
	var current *section
	pendingComment := ""
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			pendingComment = strings.TrimSpace(line[1:])
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections = append(sections, section{
				name: strings.TrimSpace(line[1 : len(line)-1]), comment: pendingComment,
				values: make(map[string]string),
			})
			current = &sections[len(sections)-1]
			pendingComment = ""
			continue
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found {
			current.values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	var definitions []Definition
	var experience []uint32
	seen := make(map[uint32]struct{})
	for _, parsed := range sections {
		if strings.EqualFold(parsed.name, "PetExperience") {
			for _, value := range strings.Split(parsed.values["exptable"], ",") {
				threshold, err := parseUint32AllowZero(strings.TrimSpace(value), "pet experience threshold")
				if err != nil {
					return nil, nil, err
				}
				experience = append(experience, threshold)
			}
			continue
		}
		match := petSectionPattern.FindStringSubmatch(parsed.name)
		if match == nil {
			continue
		}
		petTypeID, err := parseUint32(match[1], "pet type ID")
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seen[petTypeID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate pet type ID %d", petTypeID)
		}
		seen[petTypeID] = struct{}{}
		level1, err := parseUint32(parsed.values["level1"], fmt.Sprintf("pet %d Level1", petTypeID))
		if err != nil {
			return nil, nil, err
		}
		level4, err := parseUint32(parsed.values["level4"], fmt.Sprintf("pet %d Level4", petTypeID))
		if err != nil {
			return nil, nil, err
		}
		level7, err := parseUint32(parsed.values["level7"], fmt.Sprintf("pet %d Level7", petTypeID))
		if err != nil {
			return nil, nil, err
		}
		if parsed.comment == "" {
			return nil, nil, fmt.Errorf("pet type %d has no adjacent name comment", petTypeID)
		}
		source := SourceClientConfig
		if local, reserved := localModelFamily(petTypeID); reserved {
			expectedLevel7 := local.AdultResourceID
			if expectedLevel7 == 0 {
				expectedLevel7 = local.JuvenileResourceID
			}
			if parsed.comment != local.Name || level1 != localStarterModelID || level4 != local.JuvenileResourceID || level7 != expectedLevel7 {
				return nil, nil, fmt.Errorf(
					"reserved local pet type %d does not match mapping %s (%d/%d/%d)",
					petTypeID, local.Name, localStarterModelID, local.JuvenileResourceID, expectedLevel7,
				)
			}
			source = SourceLocalExtension
		}
		definitions = append(definitions, Definition{
			PetTypeID: petTypeID, Name: parsed.comment,
			Level1ResourceID: level1, Level4ResourceID: level4, Level7ResourceID: level7,
			Source: source, Assignable: true,
		})
	}
	if len(definitions) == 0 {
		return nil, nil, fmt.Errorf("no PetNNNNN sections found")
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].PetTypeID < definitions[j].PetTypeID })
	return definitions, experience, nil
}

func parseUint32(value, field string) (uint32, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is missing", field)
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%s %q is invalid", field, value)
	}
	return uint32(parsed), nil
}

func parseUint32AllowZero(value, field string) (uint32, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is missing", field)
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid", field, value)
	}
	return uint32(parsed), nil
}

func auditResourceIDs(clientRoot string, archive *itemcatalog.Archive, resourceIDs ...uint32) ([]string, []string) {
	unique := make(map[uint32]struct{})
	for _, resourceID := range resourceIDs {
		if resourceID != 0 {
			unique[resourceID] = struct{}{}
		}
	}
	ids := make([]uint32, 0, len(unique))
	for resourceID := range unique {
		ids = append(ids, resourceID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var installed, missing []string
	for _, resourceID := range ids {
		for _, motion := range []string{"stand", "walk"} {
			relative := filepath.Join("object", "pet", fmt.Sprintf("pet%d_%s.img", resourceID, motion))
			archivePath := strings.ReplaceAll(relative, string(filepath.Separator), "\\")
			if fileInfo, err := os.Stat(filepath.Join(clientRoot, relative)); err == nil && !fileInfo.IsDir() || archive != nil && archive.Has(archivePath) {
				installed = append(installed, filepath.ToSlash(relative))
			} else {
				missing = append(missing, filepath.ToSlash(relative))
			}
		}
	}
	return installed, missing
}

func resourceStatus(starterResourceID uint32, installed, missing []string) string {
	if len(missing) == 0 {
		return ResourceComplete
	}
	prefix := fmt.Sprintf("object/pet/pet%d_", starterResourceID)
	starterFiles := 0
	for _, path := range installed {
		if strings.HasPrefix(path, prefix) {
			starterFiles++
		}
	}
	if starterFiles == 2 {
		return ResourceStarterOnly
	}
	return ResourceMissing
}
