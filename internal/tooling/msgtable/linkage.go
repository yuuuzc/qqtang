package msgtable

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type CommandFamilyCandidate struct {
	Command             uint16         `json:"command"`
	CommandHex          string         `json:"command_hex"`
	TransportName       string         `json:"transport_name"`
	NamePrefixDirection string         `json:"name_prefix_direction"`
	FamilyName          string         `json:"family_name"`
	MatchBasis          string         `json:"match_basis"`
	Members             []FamilyMember `json:"members"`
}

type LinkCatalog struct {
	SchemaVersion  int                      `json:"schema_version"`
	GeneratedUTC   string                   `json:"generated_utc"`
	Source         Source                   `json:"source"`
	CandidateCount int                      `json:"candidate_count"`
	Candidates     []CommandFamilyCandidate `json:"candidates"`
}

// BuildCommandFamilyCandidates performs only strict normalized-name matching.
// It is a navigation aid, never direction or wire-schema proof. Runtime
// captures and client handlers remain authoritative.
func BuildCommandFamilyCandidates(commands TransportTable, families FamilyCatalog) LinkCatalog {
	byNormalizedName := make(map[string][]MessageFamily)
	for _, family := range families.Families {
		key := normalizeFamilyName(family.Name)
		if key != "" {
			byNormalizedName[key] = append(byNormalizedName[key], family)
		}
	}
	var candidates []CommandFamilyCandidate
	for _, command := range commands.Commands {
		key := normalizeTransportName(command.Name)
		for _, family := range byNormalizedName[key] {
			candidates = append(candidates, CommandFamilyCandidate{
				Command: command.Command, CommandHex: command.CommandHex,
				TransportName: command.Name, NamePrefixDirection: command.NamePrefixDirection,
				FamilyName: family.Name, MatchBasis: "normalized_name_only",
				Members: append([]FamilyMember(nil), family.Members...),
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Command != candidates[j].Command {
			return candidates[i].Command < candidates[j].Command
		}
		if candidates[i].TransportName != candidates[j].TransportName {
			return candidates[i].TransportName < candidates[j].TransportName
		}
		return candidates[i].FamilyName < candidates[j].FamilyName
	})
	return LinkCatalog{
		SchemaVersion: 1, GeneratedUTC: time.Now().UTC().Format(time.RFC3339), Source: commands.Source,
		CandidateCount: len(candidates), Candidates: candidates,
	}
}

func normalizeTransportName(name string) string {
	name = strings.TrimPrefix(name, "ID_CMS_")
	name = strings.TrimPrefix(name, "ID_SMC_")
	prefixes := []string{"REQUEST", "REQUIRE", "REQUST", "REQUIST", "RESPONSE", "NOTIFY", "NOTIYF", "ACK", "PUSH"}
	for {
		changed := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
				name = name[len(prefix):]
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	return normalizeFamilyName(name)
}

func normalizeFamilyName(name string) string {
	var normalized strings.Builder
	for _, character := range strings.ToUpper(name) {
		if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func (candidate CommandFamilyCandidate) CompactMembers() string {
	parts := make([]string, 0, len(candidate.Members))
	for _, member := range candidate.Members {
		parts = append(parts, fmt.Sprintf("%s:%s=%s", member.Role, member.Name, member.SchemaHex))
	}
	return strings.Join(parts, "; ")
}
