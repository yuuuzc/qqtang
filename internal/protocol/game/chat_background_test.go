package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChatBackgroundCodecsFollowRecoveredLayoutsAndRoomRoute(t *testing.T) {
	listPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	listPayload = binary.BigEndian.AppendUint32(listPayload, 1234)
	listPayload = binary.BigEndian.AppendUint32(listPayload, 0)
	listPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(BackgroundListCommand), listPayload...))
	request, err := DecodeLocalBackgroundListRequest(listPacket)
	if err != nil || request.UIN != 1_000_001 || request.Time != 1234 {
		t.Fatalf("background-list request = %+v, err=%v", request, err)
	}
	response, err := BuildLocalBackgroundListResponseWithReader(listPacket, []uint32{0, 3, 9}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != BackgroundListCommand || len(inspection.Payload) != 24 ||
		binary.BigEndian.Uint32(inspection.Payload[0:4]) != 1_000_001 ||
		binary.BigEndian.Uint32(inspection.Payload[4:8]) != 1234 ||
		binary.BigEndian.Uint32(inspection.Payload[8:12]) != 3 ||
		binary.BigEndian.Uint32(inspection.Payload[20:24]) != 9 {
		t.Fatalf("background-list response = command:%04X route:%d payload:%x", inspection.Command, inspection.Route, inspection.Payload)
	}

	usePayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	usePayload = binary.BigEndian.AppendUint32(usePayload, 5678)
	usePayload = binary.BigEndian.AppendUint32(usePayload, 6)
	usePacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(UseBackgroundCommand), usePayload...))
	useRequest, err := DecodeLocalUseBackgroundRequest(usePacket)
	if err != nil || useRequest.ItemID != 6 {
		t.Fatalf("use-background request = %+v, err=%v", useRequest, err)
	}
	useResponse, err := BuildLocalUseBackgroundResponse(usePacket, UseBackgroundResultSuccess)
	if err != nil {
		t.Fatal(err)
	}
	responseInspection, err := InspectLocalPacket(useResponse)
	if err != nil {
		t.Fatal(err)
	}
	if responseInspection.Command != UseBackgroundCommand || len(responseInspection.Payload) != 10 ||
		binary.BigEndian.Uint16(responseInspection.Payload[8:10]) != UseBackgroundResultSuccess {
		t.Fatalf("use-background response = %+v payload=%x", responseInspection, responseInspection.Payload)
	}

	notification, err := BuildLocalUseBackgroundNotification(usePacket, 12, useRequest)
	if err != nil {
		t.Fatal(err)
	}
	notifyInspection, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if notifyInspection.Command != UseBackgroundNotifyCommand || notifyInspection.Route != chatBackgroundRoute ||
		notifyInspection.SectionID != 12 || notifyInspection.InnerSequence != 0 || notifyInspection.RouteSequence != 0 ||
		len(notifyInspection.Payload) != 12 || binary.BigEndian.Uint32(notifyInspection.Payload[8:12]) != 6 {
		t.Fatalf("use-background notification = %+v payload=%x", notifyInspection, notifyInspection.Payload)
	}
}

func TestBuiltInChatBackgroundCatalogMatchesShippedAssets(t *testing.T) {
	ids := BuiltInChatBackgroundIDs()
	if len(ids) != 10 || ids[0] != 0 || ids[9] != 9 {
		t.Fatalf("built-in background IDs = %v", ids)
	}
	ids[0] = 99
	if !IsBuiltInChatBackgroundID(0) || IsBuiltInChatBackgroundID(10) || BuiltInChatBackgroundIDs()[0] != 0 {
		t.Fatal("built-in background catalog is mutable or accepts an absent asset")
	}
}

func TestChatBackgroundWeddingModesMatchShippedScenes(t *testing.T) {
	if mode, ok := ChatBackgroundWeddingMode(5); !ok || mode != 1 {
		t.Fatalf("Chinese wedding background = mode %d, ok %t", mode, ok)
	}
	if mode, ok := ChatBackgroundWeddingMode(7); !ok || mode != 0 {
		t.Fatalf("western wedding background = mode %d, ok %t", mode, ok)
	}
	if _, ok := ChatBackgroundWeddingMode(0); ok {
		t.Fatal("ordinary chat background entered wedding mode")
	}
}
