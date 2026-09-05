package battleai

import (
	"testing"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/battleenv"
)

type fixedRunner struct {
	logits []float32
}

func (runner fixedRunner) RunActor(_ []float32, _ []float32, _ []uint8) ([]float32, error) {
	return append([]float32(nil), runner.logits...), nil
}

func TestNeuralCandidatesUsesStableLegalActionIDs(t *testing.T) {
	grid := battleengine.Grid{Width: 5, Height: 3, Cells: make([]battleengine.Tile, 15)}
	for index := range grid.Cells {
		grid.Cells[index].FlamePassable = true
	}
	engine, err := battleengine.New(battleengine.Config{
		Seed: 1,
		Grid: grid,
		Rules: battleengine.Rules{
			TickMS: 20, RoundDurationMS: 240_000, BombFuseMS: 3_000,
			FlameDurationMS: 600, TrapDurationMS: 6_000, ActorHalfSizePixels: 10,
		},
		Participants: []battleengine.Participant{
			{PlayerID: 1, TeamID: 1, Source: battleengine.ParticipantVirtualAI, Spawn: battleengine.Cell{Row: 1, Col: 1}, SpeedPixelsPerSecond: 80, BombCapacity: 1, BombPower: 1},
			{PlayerID: 2, TeamID: 2, Source: battleengine.ParticipantHuman, Spawn: battleengine.Cell{Row: 1, Col: 3}, SpeedPixelsPerSecond: 80, BombCapacity: 1, BombPower: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := engine.Observation(1)
	if err != nil {
		t.Fatal(err)
	}
	legal, err := engine.LegalActions(1)
	if err != nil {
		t.Fatal(err)
	}
	logits := make([]float32, battleengine.DiscreteActionCount)
	logits[battleengine.ActionMoveRight] = 10
	policy := NeuralCandidates{
		Contract: Contract{
			Format: ONNXActorFormat, ContractVersion: 1, ModelArchitectureVersion: 2,
			TensorVersion: battleenv.TensorSchemaVersion,
			Channels:      battleenv.SpatialChannels, Scalars: battleenv.ScalarFeatures,
			Actions: int(battleengine.DiscreteActionCount), Height: 13, Width: 15,
			ONNXSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		Runner: fixedRunner{logits: logits},
	}
	candidates, err := policy.CandidateActionsWithSnapshot(engine.Clone(), observation, legal, 2)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := candidates[0].Action.ID()
	if !ok || id != battleengine.ActionMoveRight {
		t.Fatalf("top candidate ID = %d/%t, want move right", id, ok)
	}
}
