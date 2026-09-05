package msgtable

import (
	"encoding/binary"
	"testing"
)

func TestParsePreservesMessageAndFieldMetadata(t *testing.T) {
	const nameOffset = 192
	data := make([]byte, 512)
	putDWORD := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
	}
	putDWORD(nameOffset-56, 0x1100)
	putDWORD(nameOffset-52, 7)
	putDWORD(nameOffset-44, 0xFFFFFFFF)
	putDWORD(nameOffset-40, 0x0FBB)
	putDWORD(nameOffset-36, 12)
	putDWORD(nameOffset-32, 44)
	putDWORD(nameOffset-28, nameOffset-0xA8)
	putDWORD(nameOffset-24, 0xFFFFFFFF)
	putDWORD(nameOffset-20, 1)
	putDWORD(nameOffset-16, 1)
	putDWORD(nameOffset-12, 1)
	putDWORD(nameOffset-8, 0xFFFFFFFF)
	putDWORD(nameOffset-4, 1)
	copy(data[nameOffset:], "QQT_GAME_RESULT_DATA\x00")

	fieldNameOffset := nameOffset + recordFieldTableOffset
	putDWORD(fieldNameOffset-84, 0xFFFFFFFF)
	putDWORD(fieldNameOffset-64, 2)
	putDWORD(fieldNameOffset-60, 0)
	putDWORD(fieldNameOffset-56, 1)
	putDWORD(fieldNameOffset-52, 2)
	putDWORD(fieldNameOffset-40, 0x1234)
	putDWORD(fieldNameOffset-36, 0x5678)
	putDWORD(fieldNameOffset-16, 0xFFFFFFFF)
	copy(data[fieldNameOffset:], "PlayerID\x00")

	records, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	record := records[0]
	if record.Index != 7 || record.SchemaID != 0x0FBB || record.Name != "QQT_GAME_RESULT_DATA" {
		t.Fatalf("record = %+v", record)
	}
	if len(record.Fields) != 1 {
		t.Fatalf("field count = %d, want 1", len(record.Fields))
	}
	field := record.Fields[0]
	if field.Name != "PlayerID" || field.StorageWidth != 2 || field.ElementWidth != 2 || field.TypeCode1 != 0x1234 {
		t.Fatalf("field = %+v", field)
	}
	if len(record.HeaderRaw) != 14 || len(field.Raw) != 22 {
		t.Fatalf("raw DWORD counts = %d/%d, want 14/22", len(record.HeaderRaw), len(field.Raw))
	}
}

func TestParseRejectsDuplicateRecordIndices(t *testing.T) {
	data := make([]byte, 768)
	for _, nameOffset := range []int{192, 448} {
		binary.LittleEndian.PutUint32(data[nameOffset-56:nameOffset-52], 0x1140)
		binary.LittleEndian.PutUint32(data[nameOffset-52:nameOffset-48], 3)
		binary.LittleEndian.PutUint32(data[nameOffset-44:nameOffset-40], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(data[nameOffset-28:nameOffset-24], uint32(nameOffset-0xA8))
		binary.LittleEndian.PutUint32(data[nameOffset-24:nameOffset-20], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(data[nameOffset-16:nameOffset-12], 1)
		binary.LittleEndian.PutUint32(data[nameOffset-12:nameOffset-8], 1)
		binary.LittleEndian.PutUint32(data[nameOffset-8:nameOffset-4], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(data[nameOffset-4:nameOffset], 1)
		copy(data[nameOffset:], "DUPLICATE\x00")
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("accepted duplicate message indices")
	}
}

func TestParseIncludesCategoriesWithoutAnAllowlist(t *testing.T) {
	const nameOffset = 192
	data := make([]byte, 384)
	binary.LittleEndian.PutUint32(data[nameOffset-56:nameOffset-52], 0x1540)
	binary.LittleEndian.PutUint32(data[nameOffset-52:nameOffset-48], 191)
	binary.LittleEndian.PutUint32(data[nameOffset-44:nameOffset-40], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(data[nameOffset-40:nameOffset-36], 0x081D)
	binary.LittleEndian.PutUint32(data[nameOffset-28:nameOffset-24], uint32(nameOffset-0xA8))
	binary.LittleEndian.PutUint32(data[nameOffset-24:nameOffset-20], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(data[nameOffset-16:nameOffset-12], 1)
	binary.LittleEndian.PutUint32(data[nameOffset-12:nameOffset-8], 1)
	binary.LittleEndian.PutUint32(data[nameOffset-8:nameOffset-4], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(data[nameOffset-4:nameOffset], 1)
	copy(data[nameOffset:], "NOTIFY_PLAYER_ITEMADD\x00")
	records, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Category != 0x1540 || records[0].SchemaID != 0x081D {
		t.Fatalf("records = %+v", records)
	}
}

func TestParseTransportCommandsUsesFixedCommandNameRecords(t *testing.T) {
	data := make([]byte, 128)
	copy(data[16:48], "ID_CMS_REQUESTCHANGETERM\x00")
	binary.LittleEndian.PutUint32(data[48:52], 0x0070)
	copy(data[52:84], "ID_SMC_NOTIFYCHANGETERM\x00")
	binary.LittleEndian.PutUint32(data[84:88], 0x007A)
	commands, err := ParseTransportCommands(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0].Command != 0x0070 || commands[0].NamePrefixDirection != "client_to_server" || commands[1].Command != 0x007A || commands[1].NamePrefixDirection != "server_to_client" {
		t.Fatalf("transport commands = %+v", commands)
	}
}

func TestBuildFamiliesKeepsRolesAndPhysicalNeighbors(t *testing.T) {
	table := Table{Records: []Record{
		{Index: 1, Name: "UNRELATED", SchemaID: 0x1111},
		{Index: 2, Name: "REQUEST_CHANGE_TERM", SchemaID: 0x0400},
		{Index: 3, Name: "RESPONSE_CHANGE_TERM", SchemaID: 0x07E8},
		{Index: 4, Name: "NOTIFY_CHANGE_TERM", SchemaID: 0x0409},
		{Index: 5, Name: "ACK_CHANGE_TERM", SchemaID: 0x07F1},
	}}
	catalog := BuildFamilies(table)
	if catalog.FamilyCount != 1 || catalog.MultiRoleCount != 1 || catalog.DirectionalRows != 4 {
		t.Fatalf("family summary = %+v", catalog)
	}
	family := catalog.Families[0]
	if family.Name != "CHANGE_TERM" || family.MemberCount != 4 || family.Members[0].Role != "request" || family.Members[0].PreviousName != "UNRELATED" || family.Members[3].Role != "ack" {
		t.Fatalf("family = %+v", family)
	}
}

func TestBuildCommandFamilyCandidatesMarksNameOnlyEvidence(t *testing.T) {
	commands := TransportTable{Commands: []TransportCommand{{
		Command: 0x007F, CommandHex: "0x007F", Name: "ID_CMS_REQUESTSETSEATSTATUS", NamePrefixDirection: "client_to_server",
	}}}
	families := FamilyCatalog{Families: []MessageFamily{{
		Name: "SET_SEAT_STATUS", Members: []FamilyMember{{Role: "request", Name: "REQUEST_SET_SEAT_STATUS", SchemaHex: "0x00000417"}},
	}}}
	links := BuildCommandFamilyCandidates(commands, families)
	if links.CandidateCount != 1 || links.Candidates[0].FamilyName != "SET_SEAT_STATUS" || links.Candidates[0].MatchBasis != "normalized_name_only" {
		t.Fatalf("link candidates = %+v", links)
	}
}
