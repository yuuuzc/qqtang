package game

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	PlayerPetsRequestSchema  uint32 = 0x0BCF
	PlayerPetsResponseSchema uint32 = 0x0BD0
	HandlePetRequestSchema   uint32 = 0x0BCD
	HandlePetResponseSchema  uint32 = 0x0BCE

	PetInfoBinarySize       = 58
	PetInfoWireMinimumSize  = 38
	PetNameSlotSize         = 12
	PetSkillsSlotSize       = 20
	PlayerPetsBaseCapacity  = 5
	PlayerPetsExtraCapacity = 20
	PlayerPetsCapacity      = PlayerPetsBaseCapacity + PlayerPetsExtraCapacity
	HandlePetParameterSize  = 20
	HandlePetReasonSize     = 200
	HandlePetRequestSize    = 36
	HandlePetResponseSize   = 266

	PetStateActive   uint16 = 1
	PetStateInactive uint16 = 2
	PetMaxLoyalty    uint32 = 1000
	PetMaxMood       uint16 = 100
)

// HandlePetRequest mirrors QQTMsgData.bin's REQUEST_HANDLE_PET. Parameters
// contains a decimal canonical inventory item ID for feed/skill/card events,
// and a GBK pet name for rename.
type HandlePetRequest struct {
	UIN        uint32
	Time       uint32
	EventID    PetEventID
	PetID      uint32
	Parameters [HandlePetParameterSize]byte
}

func (request HandlePetRequest) ParameterText() (string, error) {
	return decodeLegacyGBKSlot(request.Parameters[:])
}

func (request HandlePetRequest) ParameterItemID() (uint16, error) {
	text, err := request.ParameterText()
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 16)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("handle-pet parameter %q is not a non-zero uint16 item ID", text)
	}
	return uint16(value), nil
}

func DecodeLocalHandlePetRequest(requestPacket []byte) (HandlePetRequest, error) {
	var result HandlePetRequest
	packet, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return result, err
	}
	if packet.Command != HandlePetCommand {
		return result, fmt.Errorf("handle-pet command 0x%04X, want 0x%04X", packet.Command, HandlePetCommand)
	}
	payload := packet.Plaintext[localInnerHeaderSize:]
	if len(payload) != HandlePetRequestSize {
		return result, fmt.Errorf("handle-pet request payload length %d, want %d", len(payload), HandlePetRequestSize)
	}
	result.UIN = binary.BigEndian.Uint32(payload[0:4])
	result.Time = binary.BigEndian.Uint32(payload[4:8])
	result.EventID = PetEventID(binary.BigEndian.Uint32(payload[8:12]))
	result.PetID = binary.BigEndian.Uint32(payload[12:16])
	copy(result.Parameters[:], payload[16:36])
	if result.UIN == 0 {
		return HandlePetRequest{}, fmt.Errorf("handle-pet request UIN is zero")
	}
	if !result.EventID.Valid() {
		return HandlePetRequest{}, fmt.Errorf("handle-pet event ID %d is outside 1..7", result.EventID)
	}
	return result, nil
}

func BuildLocalHandlePetResponse(requestPacket []byte, resultID HandlePetResult, reason string, pet *PetInfo) ([]byte, error) {
	return BuildLocalHandlePetResponseWithReader(requestPacket, resultID, reason, pet, nil)
}

func BuildLocalHandlePetResponseWithReader(requestPacket []byte, resultID HandlePetResult, reason string, pet *PetInfo, entropy io.Reader) ([]byte, error) {
	requestPacketData, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	request, err := DecodeLocalHandlePetRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	reasonBytes, err := encodeLegacyGBKText(reason, HandlePetReasonSize)
	if err != nil {
		return nil, fmt.Errorf("handle-pet reason: %w", err)
	}
	// QQTMsgData offsets describe the maximum decoded structure, not a fixed
	// wire image. Counted arrays are compact on the wire; fields after Reason
	// therefore immediately follow the bytes selected by ReasonLen.
	payload := make([]byte, 0, 8+len(reasonBytes)+PetInfoBinarySize)
	payload = binary.BigEndian.AppendUint16(payload, uint16(resultID))
	payload = binary.BigEndian.AppendUint32(payload, uint32(request.EventID))
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(reasonBytes)))
	payload = append(payload, reasonBytes...)
	if pet != nil {
		var encodeErr error
		payload, encodeErr = pet.appendCompactNetworkBinary(payload)
		if encodeErr != nil {
			return nil, fmt.Errorf("handle-pet response pet: %w", encodeErr)
		}
	} else {
		// PetInfo is not optional in RESPONSE_HANDLE_PET. A failed operation
		// still carries the minimum zero-valued PET_BASE_INFO record.
		payload = append(payload, make([]byte, PetInfoWireMinimumSize)...)
	}
	return buildLocalResponse(requestPacket, requestPacketData, HandlePetCommand, payload, entropy)
}

// PetInfo mirrors QQTMsgData.bin's PET_BASE_INFO. PetID is an owned instance;
// PetTypeID is a PetCfg.ini section ID, never an item/inventory ID.
type PetInfo struct {
	PetID         uint32 `json:"pet_id"`
	PetTypeID     uint32 `json:"pet_type_id"`
	PetExperience uint32 `json:"experience"`
	PetLoyalty    uint32 `json:"loyalty"`
	PetLevel      uint16 `json:"level"`
	PetMood       uint16 `json:"mood"`
	PetState      uint16 `json:"state"`
	PetName       string `json:"name"`
	Skills        []byte `json:"skills,omitempty"`
}

// DefaultPetName keeps the original type name where it fits and otherwise
// truncates only at a Unicode rune boundary to the client's 12-byte GBK slot.
func DefaultPetName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	result := ""
	for _, character := range typeName {
		candidate := result + string(character)
		if _, err := encodeLegacyGBKText(candidate, PetNameSlotSize); err != nil {
			break
		}
		result = candidate
	}
	if result == "" {
		return "宠物"
	}
	return result
}

func (pet PetInfo) Validate() error {
	if pet.PetID == 0 || pet.PetTypeID == 0 {
		return fmt.Errorf("pet ID and pet type ID must be non-zero")
	}
	if pet.PetLevel == 0 {
		return fmt.Errorf("pet level must be non-zero")
	}
	if pet.PetState != PetStateActive && pet.PetState != PetStateInactive {
		return fmt.Errorf("pet state %d is neither active nor inactive", pet.PetState)
	}
	if _, err := encodeLegacyGBKText(pet.PetName, PetNameSlotSize); err != nil {
		return fmt.Errorf("pet name: %w", err)
	}
	if len(pet.Skills) > PetSkillsSlotSize {
		return fmt.Errorf("pet has %d skills, maximum is %d", len(pet.Skills), PetSkillsSlotSize)
	}
	return nil
}

func (pet PetInfo) AppendNetworkBinary(dst []byte) ([]byte, error) {
	if err := pet.Validate(); err != nil {
		return nil, err
	}
	name, err := encodeLegacyGBKText(pet.PetName, PetNameSlotSize)
	if err != nil {
		return nil, err
	}
	start := len(dst)
	dst = append(dst, make([]byte, PetInfoBinarySize)...)
	encoded := dst[start:]
	binary.BigEndian.PutUint32(encoded[0:4], pet.PetID)
	binary.BigEndian.PutUint32(encoded[4:8], pet.PetTypeID)
	binary.BigEndian.PutUint32(encoded[8:12], pet.PetExperience)
	binary.BigEndian.PutUint32(encoded[12:16], pet.PetLoyalty)
	binary.BigEndian.PutUint16(encoded[16:18], pet.PetLevel)
	binary.BigEndian.PutUint16(encoded[18:20], pet.PetMood)
	binary.BigEndian.PutUint16(encoded[20:22], pet.PetState)
	copy(encoded[22:34], name)
	binary.BigEndian.PutUint32(encoded[34:38], uint32(len(pet.Skills)))
	copy(encoded[38:58], pet.Skills)
	return dst, nil
}

// appendCompactNetworkBinary encodes PET_BASE_INFO as it appears on the
// network. Skills is a counted array, so only SkillCount bytes are present.
// AppendNetworkBinary remains the fixed 58-byte decoded representation used
// when a caller needs the schema's maximum-sized source object; room/player
// network records compact that representation again using SkillCount.
func (pet PetInfo) appendCompactNetworkBinary(dst []byte) ([]byte, error) {
	if err := pet.Validate(); err != nil {
		return nil, err
	}
	name, err := encodeLegacyGBKText(pet.PetName, PetNameSlotSize)
	if err != nil {
		return nil, err
	}
	dst = binary.BigEndian.AppendUint32(dst, pet.PetID)
	dst = binary.BigEndian.AppendUint32(dst, pet.PetTypeID)
	dst = binary.BigEndian.AppendUint32(dst, pet.PetExperience)
	dst = binary.BigEndian.AppendUint32(dst, pet.PetLoyalty)
	dst = binary.BigEndian.AppendUint16(dst, pet.PetLevel)
	dst = binary.BigEndian.AppendUint16(dst, pet.PetMood)
	dst = binary.BigEndian.AppendUint16(dst, pet.PetState)
	nameSlot := make([]byte, PetNameSlotSize)
	copy(nameSlot, name)
	dst = append(dst, nameSlot...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(pet.Skills)))
	return append(dst, pet.Skills...), nil
}

type PlayerPetsRequest struct {
	UIN  uint32
	Time uint32
}

func DecodeLocalPlayerPetsRequest(requestPacket []byte) (PlayerPetsRequest, error) {
	var result PlayerPetsRequest
	packet, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return result, err
	}
	if packet.Command != PlayerPetsCommand {
		return result, fmt.Errorf("player-pets command 0x%04X, want 0x%04X", packet.Command, PlayerPetsCommand)
	}
	payload := packet.Plaintext[localInnerHeaderSize:]
	if len(payload) != 8 {
		return result, fmt.Errorf("player-pets request payload length %d, want 8", len(payload))
	}
	result.UIN = binary.BigEndian.Uint32(payload[0:4])
	result.Time = binary.BigEndian.Uint32(payload[4:8])
	if result.UIN == 0 {
		return PlayerPetsRequest{}, fmt.Errorf("player-pets request UIN is zero")
	}
	return result, nil
}

// BuildLocalPlayerPetsResponse emits RESPONSE_PLAYER_PETS_INFO using the
// protocol's compact counted-array wire representation. The schema's
// 1452-byte size is the maximum decoded structure, not the on-wire length.
func BuildLocalPlayerPetsResponse(requestPacket []byte, pets []PetInfo) ([]byte, error) {
	return BuildLocalPlayerPetsResponseWithReader(requestPacket, pets, nil)
}

func BuildLocalPlayerPetsResponseWithReader(requestPacket []byte, pets []PetInfo, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != PlayerPetsCommand {
		return nil, fmt.Errorf("player-pets command 0x%04X, want 0x%04X", request.Command, PlayerPetsCommand)
	}
	return buildLocalPlayerPetsPacket(requestPacket, request, pets, entropy)
}

// BuildLocalPlayerPetsRefresh creates the authoritative full-list response
// after an operation that adds or removes an entity. The legacy client does
// not always issue REQUEST_PLAYER_PETS_INFO after Event 4/7, so this uses the
// operation packet only as the authenticated route/envelope template.
func BuildLocalPlayerPetsRefresh(templatePacket []byte, pets []PetInfo) ([]byte, error) {
	template, err := decodeLocalPacket(templatePacket)
	if err != nil {
		return nil, err
	}
	return buildLocalPlayerPetsPacketWithBuilder(templatePacket, template, pets, nil, true)
}

func buildLocalPlayerPetsPacket(templatePacket []byte, template localPacket, pets []PetInfo, entropy io.Reader) ([]byte, error) {
	return buildLocalPlayerPetsPacketWithBuilder(templatePacket, template, pets, entropy, false)
}

func buildLocalPlayerPetsPacketWithBuilder(templatePacket []byte, template localPacket, pets []PetInfo, entropy io.Reader, notification bool) ([]byte, error) {
	if len(pets) > PlayerPetsCapacity {
		return nil, fmt.Errorf("player has %d pets, protocol capacity is %d", len(pets), PlayerPetsCapacity)
	}
	payload := make([]byte, 0, 2+len(pets)*PetInfoBinarySize)
	baseCount := len(pets)
	if baseCount > PlayerPetsBaseCapacity {
		baseCount = PlayerPetsBaseCapacity
	}
	payload = append(payload, byte(baseCount))
	for index := 0; index < baseCount; index++ {
		var encodeErr error
		payload, encodeErr = pets[index].appendCompactNetworkBinary(payload)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode base pet %d: %w", index, encodeErr)
		}
	}
	extraCount := len(pets) - baseCount
	payload = append(payload, byte(extraCount))
	for index := 0; index < extraCount; index++ {
		petIndex := baseCount + index
		var encodeErr error
		payload, encodeErr = pets[petIndex].appendCompactNetworkBinary(payload)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode extended pet %d: %w", index, encodeErr)
		}
	}
	if notification {
		return buildLocalNotificationFromRequest(templatePacket, template, PlayerPetsCommand, payload, entropy)
	}
	return buildLocalResponse(templatePacket, template, PlayerPetsCommand, payload, entropy)
}
