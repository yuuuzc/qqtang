package winlaunch

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
)

const (
	qqtPPPControllerLocalUINOffset      = uintptr(0x25E14)
	qqtPPPControllerLocalPlayerIDOffset = uintptr(0x25E18)
	qqtPPPControllerServerIPv4Offset    = uintptr(0x25E1C)
	qqtPPPControllerServerPortOffset    = uintptr(0x25E20)
	qqtPPPControllerObservedIPv4Offset  = uintptr(0x25E24)
	qqtPPPControllerTransportOffset     = uintptr(0x259C8)
	qqtPPPTransportStateOffset          = uintptr(0x004)
	qqtPPPTransportRetryTicksOffset     = uintptr(0x00C)
	qqtPPPTransportModeOffset           = uintptr(0x43C)
	qqtPPPTransportCallbackOffset       = uintptr(0x448)
	qqtPPPPeerCallbackOffset            = uintptr(0x43C)
	qqtPPPPeerState3RetryOffset         = uintptr(0x444)
	qqtPPPPeerState1RetryOffset         = uintptr(0x448)
	qqtPPPPeerState5KeepaliveOffset     = uintptr(0x450)
	qqtPPPPeerRendezvousIPv4Offset      = uintptr(0x458)
	qqtPPPPeerRendezvousPortOffset      = uintptr(0x45C)
	qqtPPPPeerUINOffset                 = uintptr(0x464)
	qqtPPPPeerPlayerIDOffset            = uintptr(0x468)
	qqtPPPPeerCandidateAIPv4Offset      = uintptr(0x46C)
	qqtPPPPeerCandidateAPortOffset      = uintptr(0x470)
	qqtPPPPeerCandidateBIPv4Offset      = uintptr(0x474)
	qqtPPPPeerCandidateBPortOffset      = uintptr(0x478)
	qqtPPPPeerHandshakeFlagsOffset      = uintptr(0x4898)
)

// QQTPPPEndpointState is the byte-exact IPv4/host-order-port pair stored by
// QQTPPP. IPv4Raw is retained because the legacy code passes the DWORD through
// Winsock APIs and its numeric value is otherwise easy to misread by eye.
type QQTPPPEndpointState struct {
	IPv4        string `json:"ipv4"`
	IPv4Raw     string `json:"ipv4_raw"`
	StorageForm string `json:"storage_form"`
	Port        uint16 `json:"port"`
}

// QQTPPPTransportState is the mode-1 UDP transport embedded in the controller
// owner. Field names are limited to semantics established by instructions or
// live memory; unknown fields are intentionally not promoted into this API.
type QQTPPPTransportState struct {
	Address       string `json:"address"`
	State         uint32 `json:"state"`
	RetryTicks    uint32 `json:"retry_ticks"`
	Mode          uint32 `json:"mode"`
	Callback      string `json:"callback"`
	CallbackValid bool   `json:"callback_is_controller"`
}

// QQTPPPPeerState reports one of the controller's eight fixed peer records.
// The retry counters are named after the state in which their consumers read
// them; no broader protocol meaning is inferred.
type QQTPPPPeerState struct {
	Index                int                 `json:"index"`
	Address              string              `json:"address"`
	Active               bool                `json:"active"`
	UIN                  uint32              `json:"uin"`
	PlayerID             uint16              `json:"player_id"`
	State                uint32              `json:"state"`
	StateName            string              `json:"state_name"`
	Callback             string              `json:"callback"`
	State1RetryCount     int32               `json:"state_1_retry_count"`
	State3RetryCount     int32               `json:"state_3_retry_count"`
	State5KeepaliveTicks int32               `json:"state_5_keepalive_ticks"`
	RendezvousServer     QQTPPPEndpointState `json:"rendezvous_server"`
	CandidateA           QQTPPPEndpointState `json:"candidate_a"`
	CandidateB           QQTPPPEndpointState `json:"candidate_b"`
	HandshakeFlags       uint8               `json:"handshake_flags"`
}

// QQTPPPStateSnapshot is a read-only snapshot of the active QQTPPP controller.
// It performs no injection and changes no client memory.
type QQTPPPStateSnapshot struct {
	PID                   uint32               `json:"pid"`
	QQTPPPBase            string               `json:"qqtppp_base"`
	Controller            string               `json:"controller"`
	Owner                 string               `json:"owner"`
	LocalUIN              uint32               `json:"local_uin"`
	LocalPlayerID         uint16               `json:"local_player_id"`
	Server                QQTPPPEndpointState  `json:"server"`
	ObservedIPv4          string               `json:"observed_ipv4"`
	ObservedIPv4Raw       string               `json:"observed_ipv4_raw"`
	ObservedIPv4Available bool                 `json:"observed_ipv4_available"`
	Transport             QQTPPPTransportState `json:"transport"`
	Peers                 []QQTPPPPeerState    `json:"peers"`
	Behavior              string               `json:"behavior"`
}

// InspectQQTPPPState locates the active main-interface subobject by its vtable
// and reads only the fields whose offsets have been closed statically. Empty
// peer records are included so room-slot/ghost-state diagnostics remain
// possible without another memory scan.
func InspectQQTPPPState(pid uint32, timeout time.Duration) (QQTPPPStateSnapshot, error) {
	result := QQTPPPStateSnapshot{
		PID:      pid,
		Behavior: "read-only active QQTPPP controller, mode-1 transport and eight-peer state snapshot; no client memory is modified",
	}
	if pid == 0 {
		return result, fmt.Errorf("pid must be non-zero")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	base, err := waitForModule(process, pid, "QQTPPP.dll", timeout)
	if err != nil {
		return result, err
	}
	controller, err := findQQTPPPController(process, pid, base)
	if err != nil {
		return result, err
	}
	owner := controller - 0x20
	result.QQTPPPBase = formatRemoteAddress(base)
	result.Controller = formatRemoteAddress(controller)
	result.Owner = formatRemoteAddress(owner)

	identity, ok := readRemote(process, controller+qqtPPPControllerLocalUINOffset, 0x14)
	if !ok || len(identity) != 0x14 {
		return result, fmt.Errorf("read QQTPPP controller identity and Type-1 rendezvous fields")
	}
	result.LocalUIN = binary.LittleEndian.Uint32(identity[0:4])
	result.LocalPlayerID = binary.LittleEndian.Uint16(identity[4:6])
	result.Server = decodeQQTPPPHostOrderEndpoint(identity[8:12], binary.LittleEndian.Uint16(identity[12:14]))
	// main slot 0 stores the Type-1 response IPv4 in Winsock/network byte
	// order. The adjacent configured server field uses the controller's
	// host-order representation, so the two fields must not share a decoder.
	result.ObservedIPv4, result.ObservedIPv4Raw = decodeQQTPPPNetworkOrderIPv4(identity[16:20])
	result.ObservedIPv4Available = binary.LittleEndian.Uint32(identity[16:20]) != 0

	transportAddress := controller + qqtPPPControllerTransportOffset
	transportHead, ok := readRemote(process, transportAddress, 0x10)
	if !ok || len(transportHead) != 0x10 {
		return result, fmt.Errorf("read QQTPPP mode-1 transport head")
	}
	transportTail, ok := readRemote(process, transportAddress+qqtPPPTransportModeOffset, 0x10)
	if !ok || len(transportTail) != 0x10 {
		return result, fmt.Errorf("read QQTPPP mode-1 transport callback fields")
	}
	callback := uintptr(binary.LittleEndian.Uint32(transportTail[12:16]))
	result.Transport = QQTPPPTransportState{
		Address:       formatRemoteAddress(transportAddress),
		State:         binary.LittleEndian.Uint32(transportHead[qqtPPPTransportStateOffset : qqtPPPTransportStateOffset+4]),
		RetryTicks:    binary.LittleEndian.Uint32(transportHead[qqtPPPTransportRetryTicksOffset : qqtPPPTransportRetryTicksOffset+4]),
		Mode:          binary.LittleEndian.Uint32(transportTail[0:4]),
		Callback:      formatRemoteAddress(callback),
		CallbackValid: callback == controller,
	}

	result.Peers = make([]QQTPPPPeerState, 0, 8)
	for index := 0; index < 8; index++ {
		peerAddress := controller + qqtPPPControllerPeerBase + uintptr(index)*qqtPPPPeerStride
		peer, err := readQQTPPPPeerState(process, peerAddress, index)
		if err != nil {
			return result, err
		}
		result.Peers = append(result.Peers, peer)
	}
	return result, nil
}

func readQQTPPPPeerState(process syscall.Handle, address uintptr, index int) (QQTPPPPeerState, error) {
	result := QQTPPPPeerState{Index: index, Address: formatRemoteAddress(address)}
	core, ok := readRemote(process, address+qqtPPPPeerCallbackOffset, 0x40)
	if !ok || len(core) != 0x40 {
		return result, fmt.Errorf("read QQTPPP peer %d core at %s", index, result.Address)
	}
	flags, ok := readRemote(process, address+qqtPPPPeerHandshakeFlagsOffset, 1)
	if !ok || len(flags) != 1 {
		return result, fmt.Errorf("read QQTPPP peer %d handshake flags", index)
	}
	result.Callback = formatRemoteAddress(uintptr(binary.LittleEndian.Uint32(core[0:4])))
	result.State3RetryCount = int32(binary.LittleEndian.Uint32(core[8:12]))
	result.State1RetryCount = int32(binary.LittleEndian.Uint32(core[12:16]))
	result.State5KeepaliveTicks = int32(binary.LittleEndian.Uint32(core[20:24]))
	result.RendezvousServer = decodeQQTPPPHostOrderEndpoint(core[28:32], binary.LittleEndian.Uint16(core[32:34]))
	result.State = binary.LittleEndian.Uint32(core[36:40])
	result.StateName = qqtPPPPeerStateName(result.State)
	result.UIN = binary.LittleEndian.Uint32(core[40:44])
	result.PlayerID = binary.LittleEndian.Uint16(core[44:46])
	result.CandidateA = decodeQQTPPPEndpoint(core[48:52], binary.LittleEndian.Uint16(core[52:54]))
	result.CandidateB = decodeQQTPPPEndpoint(core[56:60], binary.LittleEndian.Uint16(core[60:62]))
	result.HandshakeFlags = flags[0]
	result.Active = result.UIN != 0 && result.PlayerID != 0 && result.PlayerID != 0xffff
	return result, nil
}

func decodeQQTPPPEndpoint(ip []byte, port uint16) QQTPPPEndpointState {
	text, raw := decodeQQTPPPNetworkOrderIPv4(ip)
	return QQTPPPEndpointState{IPv4: text, IPv4Raw: raw, StorageForm: "winsock_network_order", Port: port}
}

func decodeQQTPPPHostOrderEndpoint(ip []byte, port uint16) QQTPPPEndpointState {
	text, raw := decodeQQTPPPHostOrderIPv4(ip)
	return QQTPPPEndpointState{IPv4: text, IPv4Raw: raw, StorageForm: "host_order_ulong", Port: port}
}

func decodeQQTPPPNetworkOrderIPv4(value []byte) (string, string) {
	if len(value) != net.IPv4len {
		return "", ""
	}
	raw := binary.LittleEndian.Uint32(value)
	return net.IP(value).String(), fmt.Sprintf("0x%08X", raw)
}

func decodeQQTPPPHostOrderIPv4(value []byte) (string, string) {
	if len(value) != net.IPv4len {
		return "", ""
	}
	raw := binary.LittleEndian.Uint32(value)
	ip := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(ip, raw)
	return ip.String(), fmt.Sprintf("0x%08X", raw)
}

func formatRemoteAddress(address uintptr) string {
	return fmt.Sprintf("0x%08X", address)
}

func qqtPPPPeerStateName(state uint32) string {
	switch state {
	case 0:
		return "empty"
	case 1:
		return "registered_waiting_negotiation"
	case 2:
		return "udp_ok_requested_waiting_candidate"
	case 3:
		return "handshake_retry"
	case 5:
		return "established"
	default:
		return "unknown"
	}
}
