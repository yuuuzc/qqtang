package game

import (
	"encoding/binary"
	"fmt"
)

const (
	LegacyUDPControlHeaderSize = 18
	LegacyUDPPeerHandshakeSize = 15
	LegacyUDPRelayHeaderSize   = 10
	LegacyUDPMaxDatagramSize   = 1024
	LegacyUDPMaxTargets        = 8
)

var legacyUDPRelayMarker = [4]byte{0, 0, 0, 1}

// LegacyUDPControlHeader is the common header used by QQTPPP's independent
// room-peer channel. It is not a QQT section message and must not be passed
// through the ordinary message dispatcher.
type LegacyUDPControlHeader struct {
	Flags        byte
	PacketNumber uint32
	PlayerID     uint16
	UIN          uint32
	Type         LegacyUDPMessageType
	Checksum     uint16
	RouteBytes   byte
}

// LegacyUDPEndpoint is an IPv4 endpoint encoded by the 5.2 client.
type LegacyUDPEndpoint struct {
	IPv4 [4]byte
	Port uint16
}

const (
	LegacyUDPPeerHandshakeRequest  byte = 0x20
	LegacyUDPPeerHandshakeResponse byte = 0x21
)

// LegacyUDPPeerHandshake is QQTPPP's compact direct-peer candidate probe.
// It is deliberately separate from the 18-byte Type-1/2/3 control family:
// the frame identifies only its sender and therefore has no server-side
// destination field. In the normal path peers exchange it directly. A server
// can only relay it safely to authenticated peers in the same room.
type LegacyUDPPeerHandshake struct {
	Flags    byte
	UIN      uint32
	Endpoint LegacyUDPEndpoint
}

func DecodeLegacyUDPPeerHandshake(data []byte) (LegacyUDPPeerHandshake, error) {
	var handshake LegacyUDPPeerHandshake
	if len(data) != LegacyUDPPeerHandshakeSize {
		return handshake, fmt.Errorf("legacy UDP peer handshake length %d, want %d", len(data), LegacyUDPPeerHandshakeSize)
	}
	if int(binary.BigEndian.Uint16(data[0:2])) != len(data) {
		return handshake, fmt.Errorf("legacy UDP peer handshake declared length %d != received %d", binary.BigEndian.Uint16(data[0:2]), len(data))
	}
	if legacyUDPChecksum(data) != 0 {
		return handshake, fmt.Errorf("legacy UDP peer handshake checksum 0x%04x is invalid", binary.BigEndian.Uint16(data[3:5]))
	}
	handshake = LegacyUDPPeerHandshake{
		Flags:    data[2],
		UIN:      binary.BigEndian.Uint32(data[5:9]),
		Endpoint: decodeLegacyUDPEndpoint(data[9:15]),
	}
	if handshake.Flags != LegacyUDPPeerHandshakeRequest && handshake.Flags != LegacyUDPPeerHandshakeResponse {
		return LegacyUDPPeerHandshake{}, fmt.Errorf("legacy UDP peer handshake flags 0x%02x are unsupported", handshake.Flags)
	}
	if handshake.UIN == 0 || handshake.Endpoint.IPv4 == [4]byte{} || handshake.Endpoint.Port == 0 {
		return LegacyUDPPeerHandshake{}, fmt.Errorf("legacy UDP peer handshake identity or endpoint is invalid")
	}
	return handshake, nil
}

func (handshake LegacyUDPPeerHandshake) Encode() ([]byte, error) {
	if handshake.Flags != LegacyUDPPeerHandshakeRequest && handshake.Flags != LegacyUDPPeerHandshakeResponse {
		return nil, fmt.Errorf("legacy UDP peer handshake flags 0x%02x are unsupported", handshake.Flags)
	}
	if handshake.UIN == 0 || handshake.Endpoint.IPv4 == [4]byte{} || handshake.Endpoint.Port == 0 {
		return nil, fmt.Errorf("legacy UDP peer handshake identity or endpoint is invalid")
	}
	data := make([]byte, LegacyUDPPeerHandshakeSize)
	binary.BigEndian.PutUint16(data[0:2], LegacyUDPPeerHandshakeSize)
	data[2] = handshake.Flags
	binary.BigEndian.PutUint32(data[5:9], handshake.UIN)
	copy(data[9:15], encodeLegacyUDPEndpoint(handshake.Endpoint))
	binary.BigEndian.PutUint16(data[3:5], legacyUDPChecksum(data))
	return data, nil
}

// Type 1 uses LegacyUDPEndpoint in both directions: the client request carries
// its locally selected endpoint, while the server response replaces it with
// the public/source endpoint observed by the UDP listener.

// LegacyUDPRelayDatagram is the separate 10-byte envelope consumed by one
// NetCenter ReceiveFrom wrapper. QQTPPP's active Type-1/Type-3 room-peer socket
// does not use this envelope; its callback receives raw UDP payload plus the
// transport source endpoint directly.
type LegacyUDPRelayDatagram struct {
	Source  LegacyUDPEndpoint
	Payload []byte
}

func EncodeLegacyUDPRelayDatagram(source LegacyUDPEndpoint, payload []byte) ([]byte, error) {
	if source.IPv4 == [4]byte{} || source.Port == 0 {
		return nil, fmt.Errorf("legacy UDP relay source endpoint is invalid")
	}
	if len(payload) == 0 || len(payload) > 0x2000-LegacyUDPRelayHeaderSize {
		return nil, fmt.Errorf("legacy UDP relay payload length %d is outside 1..%d", len(payload), 0x2000-LegacyUDPRelayHeaderSize)
	}
	data := make([]byte, LegacyUDPRelayHeaderSize+len(payload))
	copy(data[0:4], legacyUDPRelayMarker[:])
	copy(data[4:8], source.IPv4[:])
	binary.BigEndian.PutUint16(data[8:10], source.Port)
	copy(data[LegacyUDPRelayHeaderSize:], payload)
	return data, nil
}

func DecodeLegacyUDPRelayDatagram(data []byte) (LegacyUDPRelayDatagram, error) {
	if len(data) <= LegacyUDPRelayHeaderSize {
		return LegacyUDPRelayDatagram{}, fmt.Errorf("legacy UDP relay datagram length %d is too short", len(data))
	}
	if [4]byte(data[0:4]) != legacyUDPRelayMarker {
		return LegacyUDPRelayDatagram{}, fmt.Errorf("legacy UDP relay marker %x is invalid", data[0:4])
	}
	var source LegacyUDPEndpoint
	copy(source.IPv4[:], data[4:8])
	source.Port = binary.BigEndian.Uint16(data[8:10])
	if source.IPv4 == [4]byte{} || source.Port == 0 {
		return LegacyUDPRelayDatagram{}, fmt.Errorf("legacy UDP relay source endpoint is invalid")
	}
	return LegacyUDPRelayDatagram{Source: source, Payload: append([]byte(nil), data[LegacyUDPRelayHeaderSize:]...)}, nil
}

// LegacyUDPRoomPeerPayload carries one endpoint through the Type-3 rendezvous
// exchange. TargetPlayerID/TargetUIN select the receiver; Endpoint advertises
// a candidate for the sender identified by the outer header. The server may
// replace an untrusted advertised candidate with the authenticated source it
// observed before relaying it to the target.
type LegacyUDPRoomPeerPayload struct {
	TargetPlayerID uint16
	TargetUIN      uint32
	Endpoint       LegacyUDPEndpoint
}

// LegacyUDPMulticastTarget is the six-byte room identity written by
// QQTPPP!FUN_10006704: PlayerID followed by UIN, both in network byte order.
// Older recovery code misnamed these values Port/IPv4 because one artificial
// capture happened to contain endpoint-shaped numbers.
type LegacyUDPMulticastTarget struct {
	PlayerID uint16
	UIN      uint32
}

// LegacyUDPMulticastPayload is QQTPPP Type 2. Data is the encoded compact
// CSendPackageInRoom/BatchData payload delivered to the listed room members.
type LegacyUDPMulticastPayload struct {
	Targets []LegacyUDPMulticastTarget
	Data    []byte
}

// LegacyUDPControlPacket is one strictly decoded known control datagram.
// Exactly one payload pointer is non-nil according to Header.Type.
type LegacyUDPControlPacket struct {
	Header    LegacyUDPControlHeader
	Presence  *LegacyUDPEndpoint
	Multicast *LegacyUDPMulticastPayload
	RoomPeer  *LegacyUDPRoomPeerPayload
}

func DecodeLegacyUDPControlPacket(data []byte) (LegacyUDPControlPacket, error) {
	var packet LegacyUDPControlPacket
	if len(data) < LegacyUDPControlHeaderSize {
		return packet, fmt.Errorf("legacy UDP control length %d is shorter than header %d", len(data), LegacyUDPControlHeaderSize)
	}
	declared := int(binary.BigEndian.Uint16(data[0:2]))
	if declared != len(data) {
		return packet, fmt.Errorf("legacy UDP declared length %d != received %d", declared, len(data))
	}
	packet.Header = LegacyUDPControlHeader{
		Flags:        data[2],
		PacketNumber: binary.BigEndian.Uint32(data[3:7]),
		PlayerID:     binary.BigEndian.Uint16(data[7:9]),
		UIN:          binary.BigEndian.Uint32(data[9:13]),
		Type:         LegacyUDPMessageType(binary.BigEndian.Uint16(data[13:15])),
		Checksum:     binary.BigEndian.Uint16(data[15:17]),
		RouteBytes:   data[17],
	}
	if legacyUDPChecksum(data) != 0 {
		return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP checksum 0x%04x is invalid", packet.Header.Checksum)
	}
	if packet.Header.PlayerID == 0 || packet.Header.UIN == 0 {
		return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP sender identity player=%d UIN=%d is invalid", packet.Header.PlayerID, packet.Header.UIN)
	}
	payload := data[LegacyUDPControlHeaderSize:]
	switch packet.Header.Type {
	case LegacyUDPPresenceType:
		if int(packet.Header.RouteBytes) != len(payload) {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP presence route bytes %d != received %d", packet.Header.RouteBytes, len(payload))
		}
		if len(payload) != 6 {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP presence payload length %d, want 6", len(payload))
		}
		endpoint := decodeLegacyUDPEndpoint(payload)
		if endpoint.Port == 0 {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP presence port must be non-zero")
		}
		packet.Presence = &endpoint
	case LegacyUDPMulticastType:
		routeBytes := int(packet.Header.RouteBytes)
		if routeBytes < 6 || routeBytes%6 != 0 || routeBytes/6 > LegacyUDPMaxTargets {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP multicast route bytes %d do not encode 1..%d targets", routeBytes, LegacyUDPMaxTargets)
		}
		if routeBytes >= len(payload) {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP multicast has no BatchData after %d route bytes", routeBytes)
		}
		multicast := LegacyUDPMulticastPayload{
			Targets: make([]LegacyUDPMulticastTarget, 0, routeBytes/6),
			Data:    append([]byte(nil), payload[routeBytes:]...),
		}
		for offset := 0; offset < routeBytes; offset += 6 {
			target := decodeLegacyUDPMulticastTarget(payload[offset : offset+6])
			if target.PlayerID == 0 || target.UIN == 0 {
				return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP multicast target %d is invalid", offset/6)
			}
			multicast.Targets = append(multicast.Targets, target)
		}
		packet.Multicast = &multicast
	case LegacyUDPRoomPeerType:
		if int(packet.Header.RouteBytes) != len(payload) {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP room-peer route bytes %d != received %d", packet.Header.RouteBytes, len(payload))
		}
		if len(payload) != 12 {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP room-peer payload length %d, want 12", len(payload))
		}
		roomPeer := LegacyUDPRoomPeerPayload{
			TargetPlayerID: binary.BigEndian.Uint16(payload[0:2]),
			TargetUIN:      binary.BigEndian.Uint32(payload[2:6]),
			Endpoint:       decodeLegacyUDPEndpoint(payload[6:12]),
		}
		if roomPeer.TargetPlayerID == 0 || roomPeer.TargetUIN == 0 || roomPeer.Endpoint.Port == 0 {
			return LegacyUDPControlPacket{}, fmt.Errorf(
				"legacy UDP target player=%d UIN=%d port=%d is invalid",
				roomPeer.TargetPlayerID, roomPeer.TargetUIN, roomPeer.Endpoint.Port,
			)
		}
		if roomPeer.TargetPlayerID == packet.Header.PlayerID || roomPeer.TargetUIN == packet.Header.UIN {
			return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP room-peer packet targets its sender")
		}
		packet.RoomPeer = &roomPeer
	default:
		return LegacyUDPControlPacket{}, fmt.Errorf("legacy UDP control type %d is unsupported", packet.Header.Type)
	}
	return packet, nil
}

func (packet LegacyUDPControlPacket) Encode() ([]byte, error) {
	var payload []byte
	switch packet.Header.Type {
	case LegacyUDPPresenceType:
		if packet.Presence == nil || packet.Multicast != nil || packet.RoomPeer != nil {
			return nil, fmt.Errorf("legacy UDP presence packet has inconsistent payload")
		}
		payload = encodeLegacyUDPEndpoint(*packet.Presence)
	case LegacyUDPMulticastType:
		if packet.Multicast == nil || packet.Presence != nil || packet.RoomPeer != nil {
			return nil, fmt.Errorf("legacy UDP multicast packet has inconsistent payload")
		}
		if len(packet.Multicast.Targets) == 0 || len(packet.Multicast.Targets) > LegacyUDPMaxTargets || len(packet.Multicast.Data) == 0 {
			return nil, fmt.Errorf("legacy UDP multicast targets or BatchData are invalid")
		}
		payload = make([]byte, 0, len(packet.Multicast.Targets)*6+len(packet.Multicast.Data))
		for index, target := range packet.Multicast.Targets {
			if target.PlayerID == 0 || target.UIN == 0 {
				return nil, fmt.Errorf("legacy UDP multicast target %d is invalid", index)
			}
			payload = append(payload, encodeLegacyUDPMulticastTarget(target)...)
		}
		packet.Header.RouteBytes = byte(len(packet.Multicast.Targets) * 6)
		payload = append(payload, packet.Multicast.Data...)
	case LegacyUDPRoomPeerType:
		if packet.RoomPeer == nil || packet.Presence != nil || packet.Multicast != nil {
			return nil, fmt.Errorf("legacy UDP room-peer packet has inconsistent payload")
		}
		payload = make([]byte, 12)
		binary.BigEndian.PutUint16(payload[0:2], packet.RoomPeer.TargetPlayerID)
		binary.BigEndian.PutUint32(payload[2:6], packet.RoomPeer.TargetUIN)
		copy(payload[6:12], encodeLegacyUDPEndpoint(packet.RoomPeer.Endpoint))
	default:
		return nil, fmt.Errorf("legacy UDP control type %d is unsupported", packet.Header.Type)
	}
	if packet.Header.PlayerID == 0 || packet.Header.UIN == 0 || len(payload) > LegacyUDPMaxDatagramSize-LegacyUDPControlHeaderSize {
		return nil, fmt.Errorf("legacy UDP sender or payload is invalid")
	}
	data := make([]byte, LegacyUDPControlHeaderSize+len(payload))
	binary.BigEndian.PutUint16(data[0:2], uint16(len(data)))
	data[2] = packet.Header.Flags
	binary.BigEndian.PutUint32(data[3:7], packet.Header.PacketNumber)
	binary.BigEndian.PutUint16(data[7:9], packet.Header.PlayerID)
	binary.BigEndian.PutUint32(data[9:13], packet.Header.UIN)
	binary.BigEndian.PutUint16(data[13:15], uint16(packet.Header.Type))
	if packet.Header.Type != LegacyUDPMulticastType {
		packet.Header.RouteBytes = byte(len(payload))
	}
	data[17] = packet.Header.RouteBytes
	copy(data[LegacyUDPControlHeaderSize:], payload)
	binary.BigEndian.PutUint16(data[15:17], legacyUDPChecksum(data))
	return data, nil
}

// legacyUDPChecksum is the exact 16-bit one's-complement checksum used by
// QQTPPP!FUN_10006132. The x86 client sums little-endian words, folds carries,
// complements the result and stores the final value in network byte order.
// A complete valid datagram therefore evaluates to zero.
func legacyUDPChecksum(data []byte) uint16 {
	var sum uint32
	for offset := 0; offset+1 < len(data); offset += 2 {
		sum += uint32(binary.LittleEndian.Uint16(data[offset : offset+2]))
	}
	if len(data)%2 != 0 {
		sum += uint32(data[len(data)-1])
	}
	sum = (sum & 0xffff) + (sum >> 16)
	sum = (sum & 0xffff) + (sum >> 16)
	return ^uint16(sum)
}

func decodeLegacyUDPEndpoint(data []byte) LegacyUDPEndpoint {
	var endpoint LegacyUDPEndpoint
	copy(endpoint.IPv4[:], data[0:4])
	endpoint.Port = binary.BigEndian.Uint16(data[4:6])
	return endpoint
}

func encodeLegacyUDPEndpoint(endpoint LegacyUDPEndpoint) []byte {
	data := make([]byte, 6)
	copy(data[0:4], endpoint.IPv4[:])
	binary.BigEndian.PutUint16(data[4:6], endpoint.Port)
	return data
}

func decodeLegacyUDPMulticastTarget(data []byte) LegacyUDPMulticastTarget {
	return LegacyUDPMulticastTarget{
		PlayerID: binary.BigEndian.Uint16(data[0:2]),
		UIN:      binary.BigEndian.Uint32(data[2:6]),
	}
}

func encodeLegacyUDPMulticastTarget(target LegacyUDPMulticastTarget) []byte {
	data := make([]byte, 6)
	binary.BigEndian.PutUint16(data[0:2], target.PlayerID)
	binary.BigEndian.PutUint32(data[2:6], target.UIN)
	return data
}
