package msgtable

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type FamilyMember struct {
	Role           string `json:"role"`
	Index          uint32 `json:"index"`
	Name           string `json:"name"`
	SchemaID       uint32 `json:"schema_id"`
	SchemaHex      string `json:"schema_hex"`
	EncodedSize    uint32 `json:"encoded_size"`
	MaximumSize    uint32 `json:"maximum_size"`
	PreviousName   string `json:"previous_name,omitempty"`
	PreviousSchema string `json:"previous_schema,omitempty"`
	NextName       string `json:"next_name,omitempty"`
	NextSchema     string `json:"next_schema,omitempty"`
}

type MessageFamily struct {
	Name        string         `json:"name"`
	MemberCount int            `json:"member_count"`
	Members     []FamilyMember `json:"members"`
}

type FamilyCatalog struct {
	SchemaVersion   int             `json:"schema_version"`
	GeneratedUTC    string          `json:"generated_utc"`
	Source          Source          `json:"source"`
	FamilyCount     int             `json:"family_count"`
	MultiRoleCount  int             `json:"multi_role_count"`
	DirectionalRows int             `json:"directional_record_count"`
	Families        []MessageFamily `json:"families"`
}

// BuildFamilies groups every direction-named schema without inventing field
// meanings. Singleton families remain visible, and each member carries its
// physical neighbors so a newly observed schema can be examined in context.
func BuildFamilies(table Table) FamilyCatalog {
	groups := make(map[string][]FamilyMember)
	directionalRows := 0
	for index, record := range table.Records {
		role, family, ok := directionalName(record.Name)
		if !ok {
			continue
		}
		directionalRows++
		member := FamilyMember{
			Role: role, Index: record.Index, Name: record.Name,
			SchemaID: record.SchemaID, SchemaHex: fmt.Sprintf("0x%08X", record.SchemaID),
			EncodedSize: record.EncodedSize, MaximumSize: record.MaximumSize,
		}
		if index > 0 {
			member.PreviousName = table.Records[index-1].Name
			member.PreviousSchema = fmt.Sprintf("0x%08X", table.Records[index-1].SchemaID)
		}
		if index+1 < len(table.Records) {
			member.NextName = table.Records[index+1].Name
			member.NextSchema = fmt.Sprintf("0x%08X", table.Records[index+1].SchemaID)
		}
		groups[family] = append(groups[family], member)
	}
	families := make([]MessageFamily, 0, len(groups))
	multiRoleCount := 0
	for name, members := range groups {
		sort.Slice(members, func(i, j int) bool {
			if roleOrder(members[i].Role) != roleOrder(members[j].Role) {
				return roleOrder(members[i].Role) < roleOrder(members[j].Role)
			}
			return members[i].Index < members[j].Index
		})
		if len(members) > 1 {
			multiRoleCount++
		}
		families = append(families, MessageFamily{Name: name, MemberCount: len(members), Members: members})
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Name < families[j].Name })
	return FamilyCatalog{
		SchemaVersion: 1, GeneratedUTC: time.Now().UTC().Format(time.RFC3339), Source: table.Source,
		FamilyCount: len(families), MultiRoleCount: multiRoleCount, DirectionalRows: directionalRows, Families: families,
	}
}

func directionalName(name string) (string, string, bool) {
	prefixes := []struct {
		Prefix string
		Role   string
	}{
		{"REQUEST_", "request"},
		{"REQUST_", "request"},
		{"RESPONSE_", "response"},
		{"NOTIFY_", "notify"},
		{"NOTIYF_", "notify"},
		{"ACK_", "ack"},
	}
	for _, candidate := range prefixes {
		if strings.HasPrefix(name, candidate.Prefix) && len(name) > len(candidate.Prefix) {
			return candidate.Role, name[len(candidate.Prefix):], true
		}
	}
	return "", "", false
}

func roleOrder(role string) int {
	switch role {
	case "request":
		return 0
	case "response":
		return 1
	case "notify":
		return 2
	case "ack":
		return 3
	default:
		return 4
	}
}
