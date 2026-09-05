package battlereplay

import (
	"encoding/binary"
	"testing"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/qbv"
	"qqtang/internal/protocol/game"
)

func TestBuildTimelineNativeMovementAndBomb(t *testing.T) {
	movementBody, err := (game.PlayerMove{PlayerID: 1, Entries: []game.PlayerMoveEntry{{
		PlayerID: 1,
		Move:     game.PlayerMoveSequence{CurrentPosX: 60, CurrentPosY: 100, WalkAndDirection: 0x12, Speed: 5},
	}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	bombBody := make([]byte, 0, game.PlayerUseBombBodySize)
	bombBody = binary.BigEndian.AppendUint16(bombBody, 1)
	bombBody = binary.BigEndian.AppendUint32(bombBody, 3200)
	bombBody = binary.BigEndian.AppendUint32(bombBody, 9)
	bombBody = append(bombBody, 2, 1)
	bombBody = binary.BigEndian.AppendUint16(bombBody, 3)
	bombBody = append(bombBody, 0)
	timeline, err := BuildTimeline(qbv.Recording{Events: []qbv.Event{
		{TimeMS: 3000, Schema: game.PlayerMoveSchema, Data: movementBody},
		{TimeMS: 3200, Schema: game.PlayerUseBomb, Data: bombBody},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Inputs) != 2 || timeline.Inputs[0].Move != battleengine.DirectionLeft ||
		timeline.Inputs[0].MoveEnd != (battleengine.Position{}) || timeline.Inputs[1].Kind != InputPlaceBomb {
		t.Fatalf("unexpected inputs: %+v", timeline.Inputs)
	}
	if len(timeline.Checkpoints) != 1 || timeline.Checkpoints[0].Cell != (battleengine.Cell{Row: 2, Col: 1}) {
		t.Fatalf("unexpected checkpoints: %+v", timeline.Checkpoints)
	}
}

func TestBuildTimelineStopsNativeMovementAtEncodedEndPosition(t *testing.T) {
	movementBody, err := (game.PlayerMove{PlayerID: 1, Entries: []game.PlayerMoveEntry{{
		PlayerID: 1,
		Move: game.PlayerMoveSequence{
			CurrentPosX: 300, CurrentPosY: 380,
			EndPosX: 300, EndPosY: 380,
			WalkAndDirection: 0x11, Speed: 5,
		},
	}}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := BuildTimeline(qbv.Recording{Events: []qbv.Event{{
		TimeMS: 3_312, Schema: game.PlayerMoveSchema, Data: movementBody,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Inputs) != 1 || timeline.Inputs[0].Move != battleengine.DirectionNone {
		t.Fatalf("terminal movement inputs = %+v, want stopped direction", timeline.Inputs)
	}
}

func TestBuildTimelineDeduplicatesUseRequestAndConfirmation(t *testing.T) {
	use := game.WorldUseItemEvent{PlayerID: 2, ClientTime: 3_500, ItemID: 43, PosX: 100, PosY: 140}
	body, err := use.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := BuildTimeline(qbv.Recording{Events: []qbv.Event{
		{TimeMS: 3_500, Schema: game.RequestUseItem, Data: body},
		{TimeMS: 3_510, Schema: game.NotifyPlayerUseItem, Data: body},
		{TimeMS: 3_520, Schema: game.NotifyPlayerAffection, Data: body},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Inputs) != 1 || timeline.Inputs[0].Kind != InputUseAction || timeline.Inputs[0].PlayerID != 2 || timeline.Inputs[0].UseActionID != 43 {
		t.Fatalf("use-item inputs = %+v, want one action 43 pulse", timeline.Inputs)
	}
}

func TestBuildTimelineDeduplicatesPickupRequestAndConfirmation(t *testing.T) {
	body := make([]byte, 0, 14)
	body = binary.BigEndian.AppendUint16(body, 1)
	body = binary.BigEndian.AppendUint32(body, 8_938)
	body = binary.BigEndian.AppendUint32(body, 115)
	body = binary.BigEndian.AppendUint16(body, 525)
	body = binary.BigEndian.AppendUint16(body, 180)
	timeline, err := BuildTimeline(qbv.Recording{Events: []qbv.Event{
		{TimeMS: 8_938, Schema: game.RequestGetItem, Data: body},
		{TimeMS: 8_938, Schema: game.NotifyPlayerGetItem, Data: body},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Inputs) != 1 || timeline.Inputs[0].Kind != InputPickup || timeline.Inputs[0].PlayerID != 1 || timeline.Inputs[0].SceneID != 115 {
		t.Fatalf("pickup inputs = %+v, want one SceneID 115 confirmation", timeline.Inputs)
	}
}
