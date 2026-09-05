package battleengine

import (
	"reflect"
	"testing"
	"time"
)

func TestRuntimeConcurrentPoliciesOverlapAndPreservePlayerOrder(t *testing.T) {
	config := testConfig()
	config.Participants[0].Source = ParticipantVirtualAI
	config.Participants[1].Source = ParticipantVirtualAI
	reached := make(chan uint16, 2)
	release := make(chan struct{})
	policies := make(map[uint16]Policy, 2)
	for _, playerID := range []uint16{1, 2} {
		id := playerID
		policies[id] = PolicyFunc(func(Observation, []Action) (Action, error) {
			reached <- id
			<-release
			return Action{PlayerID: id}, nil
		})
	}
	runtime, err := NewRuntime(mustEngine(t, config), policies)
	if err != nil {
		t.Fatal(err)
	}
	type stepResult struct {
		result RuntimeStepResult
		err    error
	}
	done := make(chan stepResult, 1)
	go func() {
		result, stepErr := runtime.StepWithTraceConcurrentPolicies(nil)
		done <- stepResult{result: result, err: stepErr}
	}()

	seen := make(map[uint16]bool, 2)
	for len(seen) < 2 {
		select {
		case playerID := <-reached:
			seen[playerID] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("concurrent policies reached %v, want both players", seen)
		}
	}
	close(release)
	stepped := <-done
	if stepped.err != nil {
		t.Fatal(stepped.err)
	}
	want := []Action{{PlayerID: 1}, {PlayerID: 2}}
	if !reflect.DeepEqual(stepped.result.Actions, want) {
		t.Fatalf("concurrent policy actions = %+v, want %+v", stepped.result.Actions, want)
	}
}

func TestRuntimeDrivesOnlyVirtualParticipantsInStableOrder(t *testing.T) {
	config := testConfig()
	config.Participants = append(config.Participants,
		Participant{PlayerID: 4, TeamID: 2, Source: ParticipantVirtualAI, Spawn: Cell{Row: 2, Col: 3}, SpeedPixelsPerSecond: 80, BombCapacity: 1, BombPower: 1},
		Participant{PlayerID: 3, TeamID: 2, Source: ParticipantVirtualAI, Spawn: Cell{Row: 2, Col: 2}, SpeedPixelsPerSecond: 80, BombCapacity: 1, BombPower: 1},
	)
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	var called []uint16
	policies := map[uint16]Policy{}
	for _, playerID := range []uint16{3, 4} {
		id := playerID
		policies[id] = PolicyFunc(func(observation Observation, legal []Action) (Action, error) {
			called = append(called, observation.PlayerID)
			return Action{PlayerID: id}, nil
		})
	}
	runtime, err := NewRuntime(engine, policies)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.HumanPlayerIDs(); !reflect.DeepEqual(got, []uint16{1, 2}) {
		t.Fatalf("human IDs = %v", got)
	}
	if got := runtime.VirtualPlayerIDs(); !reflect.DeepEqual(got, []uint16{3, 4}) {
		t.Fatalf("virtual IDs = %v", got)
	}
	if _, err = runtime.Step([]Action{{PlayerID: 1, Move: DirectionRight}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []uint16{3, 4}) {
		t.Fatalf("policy order = %v", called)
	}
}

func TestRuntimeStepTraceReturnsAppliedHeldInput(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	runtime, err := NewRuntime(mustEngine(t, config), map[uint16]Policy{
		2: PolicyFunc(func(Observation, []Action) (Action, error) {
			return Action{PlayerID: 2, Move: DirectionLeft}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.StepWithTrace([]Action{{PlayerID: 1, Move: DirectionRight}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Action{{PlayerID: 1, Move: DirectionRight}, {PlayerID: 2, Move: DirectionLeft}}
	if !reflect.DeepEqual(result.Actions, want) {
		t.Fatalf("applied actions = %+v, want %+v", result.Actions, want)
	}
	if len(result.Events) == 0 {
		t.Fatal("step trace omitted engine events")
	}
}

func TestRuntimeSuspendedVirtualActorUsesNeutralInputWithoutCallingPolicy(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	policyCalls := 0
	runtime, err := NewRuntime(mustEngine(t, config), map[uint16]Policy{
		2: PolicyFunc(func(Observation, []Action) (Action, error) {
			policyCalls++
			return Action{PlayerID: 2, Move: DirectionLeft}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.SuspendVirtualActor(2, true); err != nil {
		t.Fatal(err)
	}
	if !runtime.VirtualActorSuspended(2) {
		t.Fatal("virtual actor was not marked suspended")
	}
	before := runtime.EngineSnapshot().actors[1].Position
	result, err := runtime.StepWithTrace(nil)
	if err != nil {
		t.Fatal(err)
	}
	if policyCalls != 0 || len(result.Actions) != 1 || result.Actions[0] != (Action{PlayerID: 2}) {
		t.Fatalf("suspended step = calls %d actions %+v", policyCalls, result.Actions)
	}
	if after := runtime.EngineSnapshot().actors[1].Position; after != before {
		t.Fatalf("suspended actor moved from %+v to %+v", before, after)
	}
	if err = runtime.SuspendVirtualActor(2, false); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.StepWithTrace(nil); err != nil {
		t.Fatal(err)
	}
	if policyCalls != 1 || runtime.VirtualActorSuspended(2) {
		t.Fatalf("resumed actor = calls %d suspended %t", policyCalls, runtime.VirtualActorSuspended(2))
	}
}

func TestRuntimeRejectsMissingMisboundAndIllegalPolicies(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewRuntime(engine, nil); err == nil {
		t.Fatal("runtime accepted a virtual participant without a policy")
	}
	if _, err = NewRuntime(engine, map[uint16]Policy{1: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 1}, nil
	})}); err == nil {
		t.Fatal("runtime accepted a policy bound to a human")
	}
	runtime, err := NewRuntime(engine, map[uint16]Policy{2: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 2, Move: Direction(99)}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.EngineSnapshot()
	if _, err = runtime.Step(nil); err == nil {
		t.Fatal("runtime accepted an illegal virtual action")
	}
	after := runtime.EngineSnapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("illegal virtual action mutated the engine")
	}
	if _, err = runtime.Step([]Action{{PlayerID: 2}}); err == nil {
		t.Fatal("runtime accepted external input for a virtual participant")
	}
}

func TestRuntimePolicyCannotMutateAuthoritativeObservation(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(engine, map[uint16]Policy{2: PolicyFunc(func(observation Observation, legal []Action) (Action, error) {
		observation.Grid.Cells[0].Kind = CellSolid
		observation.Actors[0].Position = Position{}
		legal[0].PlayerID = 99
		return Action{PlayerID: 2}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.Step(nil); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.EngineSnapshot()
	if snapshot.grid.Cells[0].Kind == CellSolid || snapshot.actors[0].Position == (Position{}) {
		t.Fatal("policy mutated authoritative engine state")
	}
}

func TestRuntimeHumanDepartureRemovesTargetWithoutDeathScatter(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	engine.actors[0].HeldActions[0] = HeldActionSlot{ActionID: 64, Count: 2}
	runtime, err := NewRuntime(engine, map[uint16]Policy{2: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 2}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.AcceptHumanDeparture(1); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.EngineSnapshot()
	var actor Actor
	ok := false
	for _, candidate := range snapshot.Actors() {
		if candidate.PlayerID == 1 {
			actor, ok = candidate, true
			break
		}
	}
	if !ok || actor.State != ActorEliminated {
		t.Fatalf("departed actor = %+v/%t, want eliminated", actor, ok)
	}
	if got := snapshot.Pickups(); len(got) != 0 {
		t.Fatalf("departure invented death drops: %+v", got)
	}
	if outcome := snapshot.Terminal(); !outcome.Ended || outcome.WinnerTeamID != 2 {
		t.Fatalf("departure outcome = %+v, want team 2", outcome)
	}
	if err = runtime.AcceptHumanDeparture(1); err != nil {
		t.Fatalf("duplicate departure should be idempotent: %v", err)
	}
}

func TestRuntimeNormalizesNativeBombPowerToInternalReach(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	engine := mustEngine(t, config)
	runtime, err := NewRuntime(engine, map[uint16]Policy{2: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 2}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.AcceptHumanBombPlacement(1, Cell{Row: 1, Col: 1}, 2, 0, 3_100); err != nil {
		t.Fatal(err)
	}
	bombs := runtime.EngineSnapshot().Bombs()
	if len(bombs) != 1 || bombs[0].Power != 1 {
		t.Fatalf("native wire power 2 became bombs %+v, want internal reach 1", bombs)
	}
}

func TestRuntimeRejectsNativeExplosionOwnedByNonParticipant(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	runtime, err := NewRuntime(mustEngine(t, config), map[uint16]Policy{2: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 2}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.AcceptNativeBombExplosion([]VerifiedExplodedBomb{{
		OwnerID: 99, Cell: Cell{Row: 1, Col: 1},
		BlastRowMin: 1, BlastRowMax: 1, BlastColMin: 1, BlastColMax: 1,
	}}, nil, nil)
	if err == nil {
		t.Fatal("native explosion accepted a non-participant bomb owner")
	}
}

func TestRuntimeSuppressesVirtualBombThatCreatesCertainSelfTrap(t *testing.T) {
	config := testConfig()
	config.Participants[1].Source = ParticipantVirtualAI
	for _, cell := range []Cell{{Row: 0, Col: 3}, {Row: 1, Col: 2}, {Row: 1, Col: 4}, {Row: 2, Col: 3}} {
		index := int(cell.Row)*int(config.Grid.Width) + int(cell.Col)
		config.Grid.Cells[index] = Tile{Kind: CellSolid}
	}
	engine := mustEngine(t, config)
	runtime, err := NewRuntime(engine, map[uint16]Policy{2: PolicyFunc(func(Observation, []Action) (Action, error) {
		return Action{PlayerID: 2, PlaceBomb: true}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	events, err := runtime.Step(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == EventBombPlaced && event.PlayerID == 2 {
			t.Fatalf("unsafe virtual bomb was not suppressed: %+v", event)
		}
	}
	if bombs := runtime.EngineSnapshot().Bombs(); len(bombs) != 0 {
		t.Fatalf("unsafe virtual bomb remained in engine: %+v", bombs)
	}
}
