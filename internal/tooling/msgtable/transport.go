package msgtable

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	transportNameWidth  = 32
	transportRecordSize = 4 + transportNameWidth
)

type TransportCommand struct {
	Command             uint16 `json:"command"`
	CommandHex          string `json:"command_hex"`
	Name                string `json:"name"`
	NameOffset          int    `json:"name_offset"`
	NamePrefixDirection string `json:"name_prefix_direction"`
}

type TransportTable struct {
	SchemaVersion      int                `json:"schema_version"`
	GeneratedUTC       string             `json:"generated_utc"`
	Source             Source             `json:"source"`
	CommandCount       int                `json:"command_count"`
	UniqueCommandCount int                `json:"unique_command_count"`
	Commands           []TransportCommand `json:"commands"`
}

// ParseTransportCommands extracts the fixed 32-byte name + 4-byte command
// records at the front of QQTMsgData.bin. CMS/SMC are preserved as a
// name-prefix direction hint, not promoted to observed network direction:
// legacy identifiers contain exceptions. Schema names elsewhere in the file
// do not use these prefixes, keeping the signature independent of offsets.
func ParseTransportCommands(data []byte) ([]TransportCommand, error) {
	commands := make([]TransportCommand, 0, 256)
	seenOffsets := make(map[int]struct{})
	for nameOffset := 0; nameOffset+transportRecordSize <= len(data); nameOffset++ {
		name, direction, ok := transportCommandName(data[nameOffset : nameOffset+transportNameWidth])
		if !ok {
			continue
		}
		commandOffset := nameOffset + transportNameWidth
		commandValue := binary.LittleEndian.Uint32(data[commandOffset : commandOffset+4])
		if commandValue == 0 || commandValue > 0xFFFF {
			continue
		}
		if _, duplicate := seenOffsets[nameOffset]; duplicate {
			continue
		}
		command := uint16(commandValue)
		seenOffsets[nameOffset] = struct{}{}
		commands = append(commands, TransportCommand{
			Command: command, CommandHex: fmt.Sprintf("0x%04X", command),
			Name: name, NameOffset: nameOffset, NamePrefixDirection: direction,
		})
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("no QQT transport commands found")
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].Command != commands[j].Command {
			return commands[i].Command < commands[j].Command
		}
		return commands[i].NameOffset < commands[j].NameOffset
	})
	return commands, nil
}

func transportCommandName(slot []byte) (string, string, bool) {
	zero := -1
	for index, value := range slot {
		if value == 0 {
			zero = index
			break
		}
	}
	if zero <= 0 {
		return "", "", false
	}
	name := string(slot[:zero])
	direction := ""
	switch {
	case strings.HasPrefix(name, "ID_CMS_"):
		direction = "client_to_server"
	case strings.HasPrefix(name, "ID_SMC_"):
		direction = "server_to_client"
	default:
		return "", "", false
	}
	for _, character := range name {
		if !(character == '_' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z') {
			return "", "", false
		}
	}
	return name, direction, true
}

func ReadTransportCommands(path string) (TransportTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TransportTable{}, err
	}
	commands, err := ParseTransportCommands(data)
	if err != nil {
		return TransportTable{}, err
	}
	source, err := sourceFor(path, data)
	if err != nil {
		return TransportTable{}, err
	}
	unique := make(map[uint16]struct{}, len(commands))
	for _, command := range commands {
		unique[command.Command] = struct{}{}
	}
	return TransportTable{
		SchemaVersion:      1,
		GeneratedUTC:       time.Now().UTC().Format(time.RFC3339),
		Source:             source,
		CommandCount:       len(commands),
		UniqueCommandCount: len(unique),
		Commands:           commands,
	}, nil
}
