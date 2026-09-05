package battleoracle

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"qqtang/internal/game/battleengine"
)

func TestRunMatchesCapturedNativeCheckpoint(t *testing.T) {
	config := oracleTestConfig()
	actions := []battleengine.Action{{PlayerID: 1, Move: battleengine.DirectionRight, PlaceBomb: true}}
	engine, err := battleengine.New(config)
	if err != nil {
		t.Fatal(err)
	}
	events, err := engine.Step(actions)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := Capture(engine, 1, events)
	if err != nil {
		t.Fatal(err)
	}
	// A movement-only native vector deliberately does not assert unrelated
	// state. Pointer fields preserve that distinction from an observed empty
	// collection.
	expected := Checkpoint{ElapsedMS: captured.ElapsedMS, Actors: captured.Actors}
	suite := oracleTestSuite(config, actions, expected)
	report, err := Run(suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Matches() || report.CaseCount != 1 || report.FrameCount != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunReportsSemanticDifference(t *testing.T) {
	config := oracleTestConfig()
	actions := []battleengine.Action{{PlayerID: 1, Move: battleengine.DirectionRight}}
	engine, err := battleengine.New(config)
	if err != nil {
		t.Fatal(err)
	}
	events, err := engine.Step(actions)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := Capture(engine, 1, events)
	if err != nil {
		t.Fatal(err)
	}
	actors := append([]battleengine.ActorObservation(nil), (*captured.Actors)...)
	actors[0].Position.X++
	suite := oracleTestSuite(config, actions, Checkpoint{Actors: &actors})
	report, err := Run(suite)
	if err != nil {
		t.Fatal(err)
	}
	if report.Matches() || len(report.Differences) != 1 || report.Differences[0].Field != "actors" {
		t.Fatalf("unexpected semantic diff: %+v", report)
	}
}

func TestRunDistinguishesObservedEmptyFromOmitted(t *testing.T) {
	config := oracleTestConfig()
	emptyBombs := []battleengine.Bomb{}
	suite := oracleTestSuite(config, nil, Checkpoint{Bombs: &emptyBombs})
	report, err := Run(suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Matches() {
		t.Fatalf("observed empty bombs did not match: %+v", report)
	}

	// Place a bomb while the original-client fixture still asserts an empty
	// bomb list. This must differ rather than being treated as omitted.
	suite.Cases[0].Frames[0].Actions = []battleengine.Action{{PlayerID: 1, PlaceBomb: true}}
	report, err = Run(suite)
	if err != nil {
		t.Fatal(err)
	}
	if report.Matches() || report.Differences[0].Field != "bombs" {
		t.Fatalf("observed empty bombs were not asserted: %+v", report)
	}
}

func TestDecodeRejectsUnknownAndUnversionedInput(t *testing.T) {
	valid := oracleTestSuite(oracleTestConfig(), nil, Checkpoint{ElapsedMS: uint32Pointer(100)})
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(encoded)); err != nil {
		t.Fatalf("valid suite rejected: %v", err)
	}

	unknown := strings.Replace(string(encoded), `"name":"native movement"`, `"name":"native movement","typo":1`, 1)
	if _, err := Decode(strings.NewReader(unknown)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field was not rejected: %v", err)
	}

	valid.SchemaVersion = 0
	encoded, err = json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(encoded)); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unversioned suite was not rejected: %v", err)
	}
}

func oracleTestSuite(config battleengine.Config, actions []battleengine.Action, expected Checkpoint) Suite {
	return Suite{
		SchemaVersion: SchemaVersion,
		Name:          "native movement",
		Source: NativeSource{
			ClientSHA256: strings.Repeat("a", 64),
			ImageBase:    0x00400000,
			Collector:    "unit-test",
			Functions:    []NativeFunction{{Name: "FUN_005cfc10", RVA: 0x001cfc10}},
		},
		Cases: []Case{{
			Name: "move-right", Config: config,
			Frames: []Frame{{Actions: actions, Expected: expected}},
		}},
	}
}

func oracleTestConfig() battleengine.Config {
	grid := battleengine.Grid{Width: 5, Height: 3, Cells: make([]battleengine.Tile, 15)}
	for index := range grid.Cells {
		grid.Cells[index].FlamePassable = true
	}
	return battleengine.Config{
		Seed: 1,
		Grid: grid,
		Rules: battleengine.Rules{
			TickMS: 100, RoundDurationMS: 10_000, BombFuseMS: 3_000,
			FlameDurationMS: 500, ActorHalfSizePixels: battleengine.NativeActorHalfSizePixels,
		},
		Participants: []battleengine.Participant{
			{PlayerID: 1, TeamID: 1, Source: battleengine.ParticipantHuman, Spawn: battleengine.Cell{Row: 1, Col: 1}, SpeedPixelsPerSecond: 80, BombCapacity: 2, BombPower: 2},
			{PlayerID: 2, TeamID: 2, Source: battleengine.ParticipantHuman, Spawn: battleengine.Cell{Row: 1, Col: 3}, SpeedPixelsPerSecond: 80, BombCapacity: 2, BombPower: 2},
		},
	}
}

func uint32Pointer(value uint32) *uint32 { return &value }
