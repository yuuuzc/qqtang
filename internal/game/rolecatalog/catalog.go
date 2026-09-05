package rolecatalog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var playerININame = regexp.MustCompile(`(?i)^player([0-9]+)\.ini$`)

// Action describes the client resource layers used for one native player
// animation. Values are resource identifiers inside their named layer; they
// are not canonical item IDs from itemCFG.
type Action struct {
	Name       string            `json:"name"`
	Components map[string]uint32 `json:"components"`
}

// Definition is one RoleID backed by object/player/player<ID>.ini.
type Definition struct {
	RoleID     uint16   `json:"role_id"`
	ConfigPath string   `json:"config_path"`
	Actions    []Action `json:"actions"`
}

// Catalog is the installed client's machine-readable RoleID/resource index.
type Catalog struct {
	SchemaVersion uint32       `json:"schema_version"`
	Source        string       `json:"source"`
	Definitions   []Definition `json:"definitions"`
}

// Load reads the unpacked original client tree. It deliberately does not add
// display names: the INI files prove resource composition, while names and
// Boss identities come from separate UI/history/protocol evidence.
func Load(clientRoot string) (Catalog, error) {
	playerRoot := filepath.Join(clientRoot, "object", "player")
	entries, err := os.ReadDir(playerRoot)
	if err != nil {
		return Catalog{}, fmt.Errorf("read player resource directory: %w", err)
	}
	result := Catalog{
		SchemaVersion: 1,
		Source:        "installed QQTang 5.2 object/player/player<ID>.ini",
	}
	seen := make(map[uint16]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := playerININame.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		parsed, parseErr := strconv.ParseUint(match[1], 10, 16)
		if parseErr != nil || parsed == 0 {
			return Catalog{}, fmt.Errorf("invalid player resource filename %q", entry.Name())
		}
		roleID := uint16(parsed)
		if previous, duplicate := seen[roleID]; duplicate {
			return Catalog{}, fmt.Errorf("RoleID %d appears in both %s and %s", roleID, previous, entry.Name())
		}
		seen[roleID] = entry.Name()
		actions, parseErr := parseINI(filepath.Join(playerRoot, entry.Name()))
		if parseErr != nil {
			return Catalog{}, fmt.Errorf("parse %s: %w", entry.Name(), parseErr)
		}
		result.Definitions = append(result.Definitions, Definition{
			RoleID:     roleID,
			ConfigPath: filepath.ToSlash(filepath.Join("object", "player", entry.Name())),
			Actions:    actions,
		})
	}
	if len(result.Definitions) == 0 {
		return Catalog{}, fmt.Errorf("no player<ID>.ini definitions found below %s", playerRoot)
	}
	sort.Slice(result.Definitions, func(i, j int) bool {
		return result.Definitions[i].RoleID < result.Definitions[j].RoleID
	})
	return result, nil
}

func parseINI(path string) ([]Action, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	actions := make(map[string]map[string]uint32)
	order := make([]string, 0, 8)
	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "" {
				return nil, fmt.Errorf("empty section")
			}
			if _, exists := actions[section]; !exists {
				actions[section] = make(map[string]uint32)
				order = append(order, section)
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || section == "" {
			return nil, fmt.Errorf("invalid line %q", line)
		}
		component := strings.ToLower(strings.TrimSpace(parts[0]))
		value, parseErr := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
		if parseErr != nil || component == "" {
			return nil, fmt.Errorf("invalid resource assignment %q", line)
		}
		actions[section][component] = uint32(value)
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]Action, 0, len(order))
	for _, name := range order {
		result = append(result, Action{Name: name, Components: actions[name]})
	}
	return result, nil
}
