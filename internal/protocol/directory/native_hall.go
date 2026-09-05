package directory

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

const HallNativeSize = 0x2CA96

const (
	hallServerTableOffset  = 0x524
	hallServerRecordSize   = 12
	hallChannelTableOffset = 0x1064
	maxHallServerCount     = (hallChannelTableOffset - hallServerTableOffset) / hallServerRecordSize
)

// LocalHallServerConfig is one SERVER_INFO entry advertised by the directory.
// Sections reference these entries by ServerID. Keeping auxiliary services on
// distinct IDs prevents the legacy client from treating their TCP sessions as
// replacements for the main hall connection.
type LocalHallServerConfig struct {
	ServerID      uint32
	ServerIP      netip.Addr
	ServerPort    uint16
	ServerUDPPort uint16
}

type LocalHallNativeConfig struct {
	Version           uint32
	Build             uint32
	LocationID        uint16
	LocationName      string
	ServerID          uint32
	ServerIP          netip.Addr
	ServerPort        uint16
	ServerUDPPort     uint16
	AdditionalServers []LocalHallServerConfig
	ChannelName       string
	ChannelID         uint32
	SectionName       string
	SectionID         uint16
	MaxPlayers        uint16
	CurrentPlayers    uint16
}

func DefaultLocalHallNativeConfig() LocalHallNativeConfig {
	return LocalHallNativeConfig{
		Version:    0x004F5D31,
		Build:      1,
		LocationID: 0x32E9,
		// The legacy UI consumes these fixed-width fields as GBK. The names
		// reproduce the official 2007-era hierarchy while the endpoint remains
		// loopback-only: 东部电信 / 自由频道 / 自由1区.
		LocationName:   "\xB6\xAB\xB2\xBF\xB5\xE7\xD0\xC5",
		ServerID:       1,
		ServerIP:       netip.MustParseAddr("127.0.0.1"),
		ServerPort:     18000,
		ServerUDPPort:  18000,
		ChannelName:    "\xD7\xD4\xD3\xC9\xC6\xB5\xB5\xC0",
		ChannelID:      1,
		SectionName:    "\xD7\xD4\xD3\xC9\x31\xC7\xF8",
		SectionID:      1,
		MaxPlayers:     500,
		CurrentPlayers: 100,
	}
}

// BuildLocalHallNative builds the in-memory RESPONSE_HALL_INFO layout
// described by the verified schema 0x0816 record in QQTMsgData.bin. It is an
// input object for the original QQTEncoder, not an on-the-wire message.
func BuildLocalHallNative(config LocalHallNativeConfig) ([]byte, error) {
	servers, err := localHallServers(config)
	if err != nil {
		return nil, err
	}
	if config.MaxPlayers == 0 || config.CurrentPlayers > config.MaxPlayers {
		return nil, fmt.Errorf("player counts current=%d max=%d are invalid", config.CurrentPlayers, config.MaxPlayers)
	}
	if err := validateNativeName("location", config.LocationName, 23); err != nil {
		return nil, err
	}
	if err := validateNativeName("channel", config.ChannelName, 20); err != nil {
		return nil, err
	}
	if err := validateNativeName("section", config.SectionName, 23); err != nil {
		return nil, err
	}

	buffer := make([]byte, HallNativeSize)
	// ResultID is zero (success).
	binary.LittleEndian.PutUint32(buffer[0x2:0x6], config.Version)
	binary.LittleEndian.PutUint32(buffer[0x6:0xA], config.Build)

	// LOCATION_INFO at 0x0A: count followed by 50 LOCATION entries.
	buffer[0x0A] = 1
	binary.LittleEndian.PutUint16(buffer[0x0B:0x0D], config.LocationID)
	buffer[0x0D] = byte(len(config.LocationName))
	copy(buffer[0x0E:0x25], config.LocationName)

	buffer[hallServerTableOffset-1] = byte(len(servers))
	for index, server := range servers {
		offset := hallServerTableOffset + index*hallServerRecordSize
		binary.LittleEndian.PutUint32(buffer[offset:offset+4], server.ServerID)
		// QQTEncoder's 0x0816 schema serializes this DWORD in network order,
		// while NetCenter later passes the decoded in-memory DWORD directly to
		// Winsock as an in_addr. Keep the native object in in_addr byte order.
		copy(buffer[offset+4:offset+8], server.ServerIP.AsSlice())
		binary.LittleEndian.PutUint16(buffer[offset+8:offset+10], server.ServerPort)
		binary.LittleEndian.PutUint16(buffer[offset+10:offset+12], server.ServerUDPPort)
	}

	// One CHANNEL_INFO at 0x1065 with one SECTION_INFO at +0x1A.
	buffer[0x1064] = 1
	channel := buffer[0x1065:]
	copy(channel[0x00:0x14], config.ChannelName)
	binary.LittleEndian.PutUint32(channel[0x14:0x18], config.ChannelID)
	binary.LittleEndian.PutUint16(channel[0x18:0x1A], 1)
	section := channel[0x1A:]
	section[0] = byte(len(config.SectionName))
	copy(section[0x1:0x18], config.SectionName)
	binary.LittleEndian.PutUint16(section[0x18:0x1A], config.SectionID)
	binary.LittleEndian.PutUint32(section[0x1A:0x1E], config.ServerID)
	binary.LittleEndian.PutUint16(section[0x1E:0x20], config.MaxPlayers)
	binary.LittleEndian.PutUint16(section[0x20:0x22], config.CurrentPlayers)
	// LowPoint/HighPoint remain zero; LocationID selects the local location.
	binary.LittleEndian.PutUint16(section[0x2A:0x2C], config.LocationID)
	return buffer, nil
}

func localHallServers(config LocalHallNativeConfig) ([]LocalHallServerConfig, error) {
	servers := make([]LocalHallServerConfig, 0, 1+len(config.AdditionalServers))
	servers = append(servers, LocalHallServerConfig{
		ServerID: config.ServerID, ServerIP: config.ServerIP,
		ServerPort: config.ServerPort, ServerUDPPort: config.ServerUDPPort,
	})
	servers = append(servers, config.AdditionalServers...)
	if len(servers) > maxHallServerCount {
		return nil, fmt.Errorf("server count %d exceeds native table capacity %d", len(servers), maxHallServerCount)
	}
	seen := make(map[uint32]struct{}, len(servers))
	for index, server := range servers {
		if server.ServerID == 0 {
			return nil, fmt.Errorf("server[%d] ID must be non-zero", index)
		}
		if _, exists := seen[server.ServerID]; exists {
			return nil, fmt.Errorf("server[%d] duplicates ServerID %d", index, server.ServerID)
		}
		seen[server.ServerID] = struct{}{}
		if !server.ServerIP.Is4() {
			return nil, fmt.Errorf("server[%d] IP %q is not IPv4", index, server.ServerIP)
		}
		if server.ServerPort == 0 || server.ServerUDPPort == 0 {
			return nil, fmt.Errorf("server[%d] ports must be non-zero", index)
		}
	}
	return servers, nil
}

func validateNativeName(field, value string, maximum int) error {
	if value == "" || len(value) > maximum {
		return fmt.Errorf("%s name length %d is outside 1..%d", field, len(value), maximum)
	}
	for _, value := range []byte(value) {
		if value == 0 {
			return fmt.Errorf("%s name contains NUL", field)
		}
	}
	return nil
}
