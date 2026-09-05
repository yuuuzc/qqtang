package directory

import (
	"encoding/binary"
	"fmt"
)

const LocalHallPayloadSize = 107

// BuildLocalHallPayload serializes the confirmed schema-0x0816
// RESPONSE_HALL_INFO fields used by the local directory server. The layout is
// based on the payload exported by the original client's QQTEncoder; reserved
// fields whose meaning is still unknown are kept at their observed zero value.
func BuildLocalHallPayload(config LocalHallNativeConfig) ([]byte, error) {
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

	payload := make([]byte, 0, LocalHallPayloadSize+hallServerRecordSize*len(config.AdditionalServers))
	appendUint16 := func(value uint16) {
		payload = binary.BigEndian.AppendUint16(payload, value)
	}
	appendUint32 := func(value uint32) {
		payload = binary.BigEndian.AppendUint32(payload, value)
	}

	appendUint16(0) // ResultID: success.
	appendUint32(config.Version)
	appendUint32(config.Build)

	payload = append(payload, 1) // Location count.
	appendUint16(config.LocationID)
	payload = append(payload, byte(len(config.LocationName)))
	payload = append(payload, config.LocationName...)
	payload = append(payload, 0, 0, 0, 0) // Reserved01, confirmed zero.

	payload = append(payload, byte(len(servers)))
	for _, server := range servers {
		appendUint32(server.ServerID)
		// QQTEncoder serializes the native DWORD numerically in big-endian
		// order. The decoded DWORD is then passed directly to Winsock.
		ip := server.ServerIP.As4()
		appendUint32(binary.LittleEndian.Uint32(ip[:]))
		appendUint16(server.ServerPort)
		appendUint16(server.ServerUDPPort)
	}

	payload = append(payload, 1) // Channel count.
	channelName := make([]byte, 20)
	copy(channelName, config.ChannelName)
	payload = append(payload, channelName...)
	appendUint32(config.ChannelID)
	appendUint16(1) // Section count.

	payload = append(payload, byte(len(config.SectionName)))
	payload = append(payload, config.SectionName...)
	appendUint16(config.SectionID)
	appendUint32(config.ServerID)
	appendUint16(config.MaxPlayers)
	appendUint16(config.CurrentPlayers)
	payload = append(payload, make([]byte, 8)...) // LowPoint, HighPoint.
	appendUint16(config.LocationID)
	payload = append(payload, make([]byte, 13)...) // Reserved02, confirmed zero.

	return payload, nil
}
