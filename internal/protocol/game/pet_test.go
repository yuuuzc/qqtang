package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHandlePetRequestAndResponse(t *testing.T) {
	payload := make([]byte, HandlePetRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 123)
	binary.BigEndian.PutUint32(payload[8:12], uint32(PetEventLearnSkill))
	binary.BigEndian.PutUint32(payload[12:16], 7)
	copy(payload[16:], "27001")
	requestPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(HandlePetCommand), payload...))

	request, err := DecodeLocalHandlePetRequest(requestPacket)
	if err != nil {
		t.Fatal(err)
	}
	if request.EventID != PetEventLearnSkill || request.PetID != 7 {
		t.Fatalf("decoded handle-pet request = %+v", request)
	}
	if itemID, err := request.ParameterItemID(); err != nil || itemID != 27001 {
		t.Fatalf("parameter item ID = %d, %v", itemID, err)
	}

	pet := PetInfo{
		PetID: 7, PetTypeID: 25001, PetLevel: 1, PetLoyalty: 1000,
		PetMood: 100, PetState: PetStateInactive, PetName: "酷比", Skills: []byte{1},
	}
	response, err := BuildLocalHandlePetResponseWithReader(requestPacket, HandlePetResultSuccess, "成功", &pet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != HandlePetCommand || len(inspection.Payload) != 8+4+PetInfoWireMinimumSize+1 {
		t.Fatalf("response command/payload = 0x%04X/%d", inspection.Command, len(inspection.Payload))
	}
	if got := PetEventID(binary.BigEndian.Uint32(inspection.Payload[2:6])); got != PetEventLearnSkill {
		t.Fatalf("response event ID = %d", got)
	}
	if reasonLength := binary.BigEndian.Uint16(inspection.Payload[6:8]); reasonLength != 4 {
		t.Fatalf("response reason length = %d", reasonLength)
	}
	petOffset := 8 + int(binary.BigEndian.Uint16(inspection.Payload[6:8]))
	if petTypeID := binary.BigEndian.Uint32(inspection.Payload[petOffset+4 : petOffset+8]); petTypeID != 25001 {
		t.Fatalf("response pet type ID = %d", petTypeID)
	}
}

func TestPlayerPetsResponseUsesBaseAndExtendedSlots(t *testing.T) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	request := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(PlayerPetsCommand), payload...))
	decoded, err := DecodeLocalPlayerPetsRequest(request)
	if err != nil || decoded.UIN != 1_000_001 {
		t.Fatalf("decoded request = %+v, %v", decoded, err)
	}
	pets := make([]PetInfo, 6)
	for index := range pets {
		pets[index] = PetInfo{
			PetID: uint32(index + 1), PetTypeID: uint32(25_001 + index), PetLevel: 1,
			PetLoyalty: 1000, PetMood: 100, PetState: PetStateInactive, PetName: "宠物",
		}
	}
	response, err := BuildLocalPlayerPetsResponseWithReader(request, pets, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != PlayerPetsCommand || len(inspection.Payload) != 2+len(pets)*PetInfoWireMinimumSize {
		t.Fatalf("response command/payload = 0x%04X/%d", inspection.Command, len(inspection.Payload))
	}
	extraCountOffset := 1 + PlayerPetsBaseCapacity*PetInfoWireMinimumSize
	if inspection.Payload[0] != PlayerPetsBaseCapacity || inspection.Payload[extraCountOffset] != 1 {
		t.Fatalf("base/extended counts = %d/%d", inspection.Payload[0], inspection.Payload[extraCountOffset])
	}
	extendedPetOffset := extraCountOffset + 1
	if petTypeID := binary.BigEndian.Uint32(inspection.Payload[extendedPetOffset+4 : extendedPetOffset+8]); petTypeID != pets[5].PetTypeID {
		t.Fatalf("extended pet type ID = %d", petTypeID)
	}
}

func TestPlayerPetsResponseCompactsNestedSkills(t *testing.T) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	request := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(PlayerPetsCommand), payload...))
	pets := []PetInfo{
		{PetID: 11, PetTypeID: 25_001, PetLevel: 1, PetLoyalty: 1000, PetMood: 100, PetState: PetStateActive, PetName: "酷比", Skills: []byte{3, 7}},
		{PetID: 12, PetTypeID: 25_002, PetLevel: 1, PetLoyalty: 1000, PetMood: 100, PetState: PetStateInactive, PetName: "酷比"},
	}
	response, err := BuildLocalPlayerPetsResponseWithReader(request, pets, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	secondPetOffset := 1 + PetInfoWireMinimumSize + len(pets[0].Skills)
	if got := binary.BigEndian.Uint32(inspection.Payload[secondPetOffset : secondPetOffset+4]); got != pets[1].PetID {
		t.Fatalf("second compact pet ID = %d, want %d", got, pets[1].PetID)
	}
	extraCountOffset := secondPetOffset + PetInfoWireMinimumSize
	if got := inspection.Payload[extraCountOffset]; got != 0 {
		t.Fatalf("extended count = %d, want 0", got)
	}
}

func TestPlayerPetsRefreshUsesHandlePetEnvelope(t *testing.T) {
	payload := make([]byte, HandlePetRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[8:12], uint32(PetEventRelease))
	binary.BigEndian.PutUint32(payload[12:16], 7)
	template := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(HandlePetCommand), payload...))
	refresh, err := BuildLocalPlayerPetsRefresh(template, []PetInfo{{
		PetID: 8, PetTypeID: 25_002, PetLevel: 1, PetLoyalty: 1000,
		PetMood: 100, PetState: PetStateInactive, PetName: "酷比",
	}})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(refresh)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != PlayerPetsCommand || len(inspection.Payload) != 2+PetInfoWireMinimumSize {
		t.Fatalf("refresh command/payload = 0x%04X/%d", inspection.Command, len(inspection.Payload))
	}
	templateInspection, err := InspectLocalPacket(template)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Route != templateInspection.Route || inspection.SectionID != templateInspection.SectionID || inspection.RouteSequence != 0 || inspection.InnerSequence != 0 {
		t.Fatalf("refresh route/section/sequences = %d/%d/%d/%d", inspection.Route, inspection.SectionID, inspection.RouteSequence, inspection.InnerSequence)
	}
}
