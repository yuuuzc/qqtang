package petcatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	localStarterModelID = uint32(100)
	localMappingBegin   = "; QQTang local restoration pet type mappings BEGIN"
	localMappingEnd     = "; QQTang local restoration pet type mappings END"
)

// InstallLocalTypeMappings adds a reproducible, explicitly local namespace to
// PetCfg.ini for official models whose original Tencent PetTypeIDs have not
// been recovered. It never rewrites or aliases canonical pet-card item IDs.
func InstallLocalTypeMappings(clientRoot string) (bool, error) {
	configPath := filepath.Join(clientRoot, "config", "PetCfg.ini")
	encoded, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read pet config: %w", err)
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(encoded)
	if err != nil {
		return false, fmt.Errorf("decode pet config as GBK: %w", err)
	}
	contents := string(decoded)
	withoutManaged, err := removeManagedLocalMappings(contents)
	if err != nil {
		return false, err
	}
	for _, model := range hiddenModelFamilies() {
		pattern := regexp.MustCompile(`(?im)^\s*\[Pet` + fmt.Sprint(model.LocalPetTypeID) + `\]\s*$`)
		if pattern.MatchString(withoutManaged) {
			return false, fmt.Errorf("reserved local PetTypeID %d already exists outside the managed block", model.LocalPetTypeID)
		}
	}

	lineEnding := "\n"
	if strings.Contains(contents, "\r\n") {
		lineEnding = "\r\n"
	}
	var block strings.Builder
	block.WriteString(localMappingBegin)
	block.WriteString(lineEnding)
	for _, model := range hiddenModelFamilies() {
		level7 := model.AdultResourceID
		if level7 == 0 {
			level7 = model.JuvenileResourceID
		}
		fmt.Fprintf(&block, "; %s%s[Pet%d]%sLevel1 = %d%sLevel4 = %d%sLevel7 = %d%s%s",
			model.Name, lineEnding, model.LocalPetTypeID, lineEnding,
			localStarterModelID, lineEnding, model.JuvenileResourceID, lineEnding,
			level7, lineEnding, lineEnding,
		)
	}
	block.WriteString(localMappingEnd)
	block.WriteString(lineEnding)

	updated := strings.TrimRight(withoutManaged, "\r\n") + lineEnding + lineEnding + block.String()
	definitions, _, err := parse(updated)
	if err != nil {
		return false, fmt.Errorf("validate pet config with local mappings: %w", err)
	}
	installed := make(map[uint32]Definition, len(definitions))
	for _, definition := range definitions {
		if definition.Source == SourceLocalExtension {
			installed[definition.PetTypeID] = definition
		}
	}
	if len(installed) != len(hiddenModelFamilies()) {
		return false, fmt.Errorf("validated %d local pet mappings, want %d", len(installed), len(hiddenModelFamilies()))
	}
	if updated == contents {
		return false, nil
	}
	reencoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(updated))
	if err != nil {
		return false, fmt.Errorf("encode pet config as GBK: %w", err)
	}
	if err := os.WriteFile(configPath, reencoded, 0o644); err != nil {
		return false, fmt.Errorf("write pet config: %w", err)
	}
	return true, nil
}

func removeManagedLocalMappings(contents string) (string, error) {
	begin := strings.Index(contents, localMappingBegin)
	end := strings.Index(contents, localMappingEnd)
	if begin < 0 && end < 0 {
		return contents, nil
	}
	if begin < 0 || end < begin {
		return "", fmt.Errorf("PetCfg.ini has an incomplete managed local pet mapping block")
	}
	end += len(localMappingEnd)
	if strings.Contains(contents[end:], localMappingBegin) || strings.Contains(contents[end:], localMappingEnd) {
		return "", fmt.Errorf("PetCfg.ini has multiple managed local pet mapping blocks")
	}
	return strings.TrimRight(contents[:begin], "\r\n") + strings.TrimLeft(contents[end:], "\r\n"), nil
}

func localModelFamily(petTypeID uint32) (ModelFamily, bool) {
	for _, model := range hiddenModelFamilies() {
		if model.LocalPetTypeID == petTypeID {
			return model, true
		}
	}
	return ModelFamily{}, false
}
