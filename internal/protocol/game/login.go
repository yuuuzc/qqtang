package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	LoginResponseSchema             = 0x07D5
	LoginResponseNative             = 0x2510
	SinglePlayerAdventureCardItemID = 99
	// SinglePlayerBossCardItemID is a local extension that shares item 99's
	// artwork but grants only the one-player competitive Boss start gate.
	SinglePlayerBossCardItemID = 30098
	// CompetitiveAICardItemID is a local, non-consumable room-control item.
	// When it is active in the room owner's inventory and not stored in the
	// collection cabinet, eligible ordinary competitive rooms may add virtual
	// opponents before the match starts.
	CompetitiveAICardItemID  = 30099
	LargeStaminaPotionItemID = 20043
	// LocalPermanentAvailablePeriod is the original client's permanent-item
	// sentinel. QQTSection reads AvailPeriod as a signed value when calculating
	// GetItemLeaveTime, so 0xffffffff becomes -1. The shop draws the quantity
	// label only when that value is negative; a large positive duration (the
	// former 0x7fffffff workaround) therefore made every stack look uncounted.
	LocalPermanentAvailablePeriod  = uint32(0xFFFFFFFF)
	LoginResponseKeyGameDataSize   = 32
	LoginResponseMaxReasonBytes    = 200
	LoginResponseFixedPayloadSize  = 123
	LoginRequestNicknameSlotSize   = 20
	LoginRequestFileInfoSize       = 20
	LoginRequestMaximumFileCount   = 20
	loginRequestBasePayloadSize    = 53
	loginRequestMaximumPayloadSize = 453
)

type LoginFileInfo struct {
	FileID   uint16
	Version  uint16
	FileHash [16]byte
}

// LoginRequest is REQUEST_LOGIN schema 0x03ED. The server treats account data
// as authoritative; the client-supplied profile fields remain useful for
// consistency checks and compatibility diagnostics.
type LoginRequest struct {
	OuterSequence  uint32
	RouteSequence  uint16
	EnvelopeUIN    uint32
	InnerUnknown   uint32
	InnerSequence  uint16
	Route          uint16
	Marker         uint16
	UIN            uint32
	ClientTime     uint32
	Nickname       string
	Gender         byte
	IconID         byte
	PalDialogID    uint16
	SectionID      uint16
	AttachIdentity uint32
	RoleID         byte
	ClientVersion  uint32
	ConfigFiles    []LoginFileInfo
	ClientType     uint32
	CSVersion      uint32
	Plaintext      []byte
}

// LocalLoginConfig is the typed input to RESPONSE_LOGIN schema 0x07D5.
type LocalLoginConfig struct {
	PlayerID             uint16
	Identity             uint32
	SectionID            uint16
	KeyGameData          []byte
	NumOfRoom            uint16
	MinRoomID            uint16
	GameInfo             GameInfo
	Items                []ItemInfo
	SectionMode          byte
	ExtraSectionModeInfo uint32
	SectionIdentity      uint32
	Reason               []byte
	Honor                uint32
	TaskCount            uint16
	TaskGrade            uint16
	TaskGameCount        uint16
	TaskGameFinished     uint16
	Patterns             []PatternPoint
}

func DefaultLocalLoginConfig() LocalLoginConfig {
	return DefaultPlayerProfile().ToLocalLoginConfig()
}

// DecodeLocalLoginRequest validates and decrypts a complete local game-login
// packet. It intentionally accepts only the observed local ST/key envelope and
// command 0x0064; malformed or unrelated packets are rejected without panic.
func DecodeLocalLoginRequest(packet []byte) (LoginRequest, error) {
	var request LoginRequest
	packetData, err := decodeLocalPacket(packet)
	if err != nil {
		return request, err
	}
	plaintext := packetData.Plaintext
	if len(plaintext) < localInnerHeaderSize+loginRequestBasePayloadSize {
		return request, fmt.Errorf("game login plaintext length %d is too short", len(plaintext))
	}
	if packetData.Command != LoginCommand {
		return request, fmt.Errorf("game login command 0x%04X, want 0x%04X", packetData.Command, LoginCommand)
	}
	payload := plaintext[localInnerHeaderSize:]
	if len(payload) > loginRequestMaximumPayloadSize {
		return request, fmt.Errorf("game login payload length %d exceeds %d", len(payload), loginRequestMaximumPayloadSize)
	}
	fileCount := int(binary.BigEndian.Uint16(payload[43:45]))
	if fileCount > LoginRequestMaximumFileCount {
		return request, fmt.Errorf("game login config file count %d exceeds %d", fileCount, LoginRequestMaximumFileCount)
	}
	expectedSize := loginRequestBasePayloadSize + fileCount*LoginRequestFileInfoSize
	if len(payload) != expectedSize {
		return request, fmt.Errorf("game login payload length %d, want %d for %d config files", len(payload), expectedSize, fileCount)
	}
	tailOffset := 45 + fileCount*LoginRequestFileInfoSize
	nickname, err := decodeLegacyGBKSlot(payload[8:28])
	if err != nil {
		return request, fmt.Errorf("game login nickname: %w", err)
	}
	request = LoginRequest{
		OuterSequence:  packetData.OuterSequence,
		RouteSequence:  packetData.RouteSequence,
		EnvelopeUIN:    packetData.EnvelopeUIN,
		InnerUnknown:   binary.BigEndian.Uint32(plaintext[2:6]),
		InnerSequence:  binary.BigEndian.Uint16(plaintext[6:8]),
		Route:          binary.BigEndian.Uint16(plaintext[8:10]),
		Marker:         binary.BigEndian.Uint16(plaintext[10:12]),
		UIN:            binary.BigEndian.Uint32(payload[0:4]),
		ClientTime:     binary.BigEndian.Uint32(payload[4:8]),
		Nickname:       nickname,
		Gender:         payload[28],
		IconID:         payload[29],
		PalDialogID:    binary.BigEndian.Uint16(payload[30:32]),
		SectionID:      binary.BigEndian.Uint16(payload[32:34]),
		AttachIdentity: binary.BigEndian.Uint32(payload[34:38]),
		RoleID:         payload[38],
		ClientVersion:  binary.BigEndian.Uint32(payload[39:43]),
		ConfigFiles:    make([]LoginFileInfo, fileCount),
		ClientType:     binary.BigEndian.Uint32(payload[tailOffset : tailOffset+4]),
		CSVersion:      binary.BigEndian.Uint32(payload[tailOffset+4 : tailOffset+8]),
		Plaintext:      append([]byte(nil), plaintext...),
	}
	for index := range request.ConfigFiles {
		offset := 45 + index*LoginRequestFileInfoSize
		request.ConfigFiles[index].FileID = binary.BigEndian.Uint16(payload[offset : offset+2])
		request.ConfigFiles[index].Version = binary.BigEndian.Uint16(payload[offset+2 : offset+4])
		copy(request.ConfigFiles[index].FileHash[:], payload[offset+4:offset+20])
	}
	if request.EnvelopeUIN != request.UIN {
		return LoginRequest{}, fmt.Errorf("game login envelope UIN %d != payload UIN %d", request.EnvelopeUIN, request.UIN)
	}
	return request, nil
}

// BuildLocalLoginPayload serializes the confirmed schema-0x07D5 field order.
// PatternPoints is encoded through its schema-controlled count and array.
func BuildLocalLoginPayload(uin uint32, config LocalLoginConfig) ([]byte, error) {
	return config.MarshalNetworkPayload(uin)
}

func (config LocalLoginConfig) MarshalNetworkPayload(uin uint32) ([]byte, error) {
	return NewLocalLoginResponse(uin, config).MarshalNetworkBinary()
}

func BuildLocalLoginSuccess(requestPacket []byte, config LocalLoginConfig) ([]byte, error) {
	return buildLocalLoginSuccess(requestPacket, config, nil)
}

func BuildLocalLoginSuccessWithReader(requestPacket []byte, config LocalLoginConfig, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalLoginSuccess(requestPacket, config, entropy)
}

func buildLocalLoginSuccess(requestPacket []byte, config LocalLoginConfig, entropy io.Reader) ([]byte, error) {
	request, err := DecodeLocalLoginRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := BuildLocalLoginPayload(request.UIN, config)
	if err != nil {
		return nil, err
	}
	// The directory exchange proves that this transport command identifies the
	// operation, not its request/response direction: both sides use 0x0133 and
	// select different QQTMsgData schemas. Login therefore retains 0x0064;
	// 0x0065..0x0068 are result/error codes consumed by QQTSection.
	packetData := localPacket{
		OuterSequence: request.OuterSequence,
		RouteSequence: request.RouteSequence,
		EnvelopeUIN:   request.EnvelopeUIN,
		Command:       LoginCommand,
		Plaintext:     request.Plaintext,
	}
	return buildLocalResponse(requestPacket, packetData, LoginCommand, payload, entropy)
}
