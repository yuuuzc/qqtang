// Package msgtable extracts the self-describing message metadata embedded in
// the legacy QQTMsgData.bin shipped with the client.
package msgtable

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion          = 1
	recordNameFromCategory = 56
	recordFieldCountOffset = -20
	recordFieldTableOffset = 0x78
	fieldStride            = 0x98
	maxFieldCount          = 128
)

// RawDWORD preserves an uninterpreted 32-bit metadata value. RelativeOffset
// is relative to the message or field name that follows the metadata block.
type RawDWORD struct {
	RelativeOffset int    `json:"relative_offset"`
	Value          uint32 `json:"value"`
	Hex            string `json:"hex"`
}

// Field contains both the confirmed layout properties and every raw DWORD
// preceding a field name. Raw values remain available when a type-code
// interpretation is refined later.
type Field struct {
	Ordinal              int        `json:"ordinal"`
	Name                 string     `json:"name"`
	NameOffset           int        `json:"name_offset"`
	TypeDefinitionOffset uint32     `json:"type_definition_offset"`
	StorageWidth         uint32     `json:"storage_width"`
	ByteOffset           uint32     `json:"byte_offset"`
	Capacity             uint32     `json:"capacity"`
	ElementWidth         uint32     `json:"element_width"`
	AdditionalTypeRef    uint32     `json:"additional_type_ref"`
	TypeCode1            uint32     `json:"type_code_1"`
	TypeCode2            uint32     `json:"type_code_2"`
	CountFieldByteOffset uint32     `json:"count_field_byte_offset"`
	DynamicCount         uint32     `json:"dynamic_count"`
	Raw                  []RawDWORD `json:"raw_dwords"`
}

// Record is one message or nested data definition in QQTMsgData.bin.
type Record struct {
	Index           uint32     `json:"index"`
	Name            string     `json:"name"`
	NameOffset      int        `json:"name_offset"`
	Category        uint32     `json:"category"`
	UnknownFlags1   uint32     `json:"unknown_flags_1"`
	UnknownFlags2   uint32     `json:"unknown_flags_2"`
	SchemaID        uint32     `json:"schema_id"`
	EncodedSize     uint32     `json:"encoded_size"`
	MaximumSize     uint32     `json:"maximum_size"`
	ReferenceOffset uint32     `json:"reference_offset"`
	DeclaredFields  uint32     `json:"declared_fields"`
	HeaderRaw       []RawDWORD `json:"header_raw_dwords"`
	Fields          []Field    `json:"fields"`
}

// Source identifies the exact binary used to generate a table.
type Source struct {
	Path     string `json:"path"`
	Size     int    `json:"size"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified_utc,omitempty"`
}

// Table is the stable JSON root emitted by the extractor.
type Table struct {
	SchemaVersion int      `json:"schema_version"`
	GeneratedUTC  string   `json:"generated_utc"`
	Source        Source   `json:"source"`
	RecordCount   int      `json:"record_count"`
	Records       []Record `json:"records"`
}

// Parse extracts every record by its complete metadata-header signature. The
// binary uses several categories (including 0x1100, 0x1140, 0x1500 and
// 0x1540), so category allowlists silently lose valid messages and nested
// types. Header relationships and field metadata provide the stronger test.
func Parse(data []byte) ([]Record, error) {
	var records []Record
	seenNames := make(map[int]struct{})
	for nameOffset := recordNameFromCategory; nameOffset < len(data); nameOffset++ {
		if !isRecordHeader(data, nameOffset) {
			continue
		}
		record, ok := parseRecord(data, nameOffset)
		if !ok {
			continue
		}
		if _, duplicate := seenNames[nameOffset]; duplicate {
			continue
		}
		seenNames[nameOffset] = struct{}{}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no QQT message metadata records found")
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Index != records[j].Index {
			return records[i].Index < records[j].Index
		}
		return records[i].NameOffset < records[j].NameOffset
	})
	seenIndices := make(map[uint32]string, len(records))
	for _, record := range records {
		if previous, exists := seenIndices[record.Index]; exists {
			return nil, fmt.Errorf("duplicate record index %d: %s and %s", record.Index, previous, record.Name)
		}
		seenIndices[record.Index] = record.Name
	}
	return records, nil
}

func isRecordHeader(data []byte, nameOffset int) bool {
	if nameOffset < recordNameFromCategory || nameOffset >= len(data) {
		return false
	}
	// Every real record points to the metadata block 0xA8 bytes before its
	// name and carries this invariant tail. Requiring all values avoids
	// treating field definitions or ordinary strings as top-level records.
	return dwordAt(data, nameOffset-48) == 0 &&
		dwordAt(data, nameOffset-44) == ^uint32(0) &&
		dwordAt(data, nameOffset-28) == uint32(nameOffset-0xA8) &&
		dwordAt(data, nameOffset-24) == ^uint32(0) &&
		dwordAt(data, nameOffset-16) == 1 &&
		dwordAt(data, nameOffset-12) == 1 &&
		dwordAt(data, nameOffset-8) == ^uint32(0) &&
		dwordAt(data, nameOffset-4) == 1
}

func parseRecord(data []byte, nameOffset int) (Record, bool) {
	if nameOffset < recordNameFromCategory || nameOffset+1 >= len(data) {
		return Record{}, false
	}
	name, ok := metadataName(data, nameOffset)
	if !ok {
		return Record{}, false
	}
	fieldCountOffset := nameOffset + recordFieldCountOffset
	if fieldCountOffset < 0 || fieldCountOffset+4 > len(data) {
		return Record{}, false
	}
	fieldCount := binary.LittleEndian.Uint32(data[fieldCountOffset : fieldCountOffset+4])
	if fieldCount > maxFieldCount {
		return Record{}, false
	}
	record := Record{
		Index:           dwordAt(data, nameOffset-52),
		Name:            name,
		NameOffset:      nameOffset,
		Category:        dwordAt(data, nameOffset-56),
		UnknownFlags1:   dwordAt(data, nameOffset-48),
		UnknownFlags2:   dwordAt(data, nameOffset-44),
		SchemaID:        dwordAt(data, nameOffset-40),
		EncodedSize:     dwordAt(data, nameOffset-36),
		MaximumSize:     dwordAt(data, nameOffset-32),
		ReferenceOffset: dwordAt(data, nameOffset-28),
		DeclaredFields:  fieldCount,
		HeaderRaw:       rawWords(data, nameOffset, -56, -4),
		Fields:          make([]Field, 0, fieldCount),
	}
	for index := 0; index < int(fieldCount); index++ {
		fieldNameOffset := nameOffset + recordFieldTableOffset + index*fieldStride
		fieldName, valid := metadataName(data, fieldNameOffset)
		if !valid || fieldNameOffset < 88 {
			return Record{}, false
		}
		record.Fields = append(record.Fields, Field{
			Ordinal:              index,
			Name:                 fieldName,
			NameOffset:           fieldNameOffset,
			TypeDefinitionOffset: dwordAt(data, fieldNameOffset-84),
			StorageWidth:         dwordAt(data, fieldNameOffset-64),
			ByteOffset:           dwordAt(data, fieldNameOffset-60),
			Capacity:             dwordAt(data, fieldNameOffset-56),
			ElementWidth:         dwordAt(data, fieldNameOffset-52),
			AdditionalTypeRef:    dwordAt(data, fieldNameOffset-48),
			TypeCode1:            dwordAt(data, fieldNameOffset-40),
			TypeCode2:            dwordAt(data, fieldNameOffset-36),
			CountFieldByteOffset: dwordAt(data, fieldNameOffset-16),
			DynamicCount:         dwordAt(data, fieldNameOffset-12),
			Raw:                  rawWords(data, fieldNameOffset, -88, -4),
		})
	}
	return record, true
}

func metadataName(data []byte, offset int) (string, bool) {
	if offset < 0 || offset >= len(data) {
		return "", false
	}
	end := offset
	for end < len(data) && end-offset <= 127 && data[end] != 0 {
		character := data[end]
		if !(character == '_' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return "", false
		}
		end++
	}
	if end == offset || end >= len(data) || end-offset > 127 || data[end] != 0 {
		return "", false
	}
	return string(data[offset:end]), true
}

func dwordAt(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset : offset+4])
}

func rawWords(data []byte, base, first, last int) []RawDWORD {
	words := make([]RawDWORD, 0, (last-first)/4+1)
	for relative := first; relative <= last; relative += 4 {
		value := dwordAt(data, base+relative)
		words = append(words, RawDWORD{RelativeOffset: relative, Value: value, Hex: fmt.Sprintf("0x%08X", value)})
	}
	return words
}

func sourceFor(path string, data []byte) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Source{}, err
	}
	digest := sha256.Sum256(data)
	return Source{
		Path:     strings.ReplaceAll(path, "\\", "/"),
		Size:     len(data),
		SHA256:   strings.ToUpper(hex.EncodeToString(digest[:])),
		Modified: info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

// Read parses a binary and creates its provenance-bearing table root.
func Read(path string) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Table{}, err
	}
	records, err := Parse(data)
	if err != nil {
		return Table{}, err
	}
	source, err := sourceFor(path, data)
	if err != nil {
		return Table{}, err
	}
	return Table{
		SchemaVersion: SchemaVersion,
		GeneratedUTC:  time.Now().UTC().Format(time.RFC3339),
		Source:        source,
		RecordCount:   len(records),
		Records:       records,
	}, nil
}
