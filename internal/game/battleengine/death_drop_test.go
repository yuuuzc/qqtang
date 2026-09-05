package battleengine

import "testing"

func TestEliminationScattersHeldActionsIntoAuthoritativeWorld(t *testing.T) {
	config := testConfig()
	engine := mustEngine(t, config)
	actor := &engine.actors[0]
	actor.State = ActorTrapped
	actor.HeldActions[0] = HeldActionSlot{ActionID: 41, Count: 2}
	actor.HeldActions[1] = HeldActionSlot{ActionID: 42, Count: 1}

	events, err := engine.ConfirmTrappedDeath(actor.PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if actor.State != ActorEliminated {
		t.Fatalf("actor state = %d, want eliminated", actor.State)
	}
	if actor.HeldActions != ([NativeBattleActionSlots]HeldActionSlot{}) {
		t.Fatalf("held actions survived death: %+v", actor.HeldActions)
	}
	if got := countEventKind(events, EventPickupDropped); got != 3 {
		t.Fatalf("drop events = %d, want 3: %+v", got, events)
	}
	if len(engine.pickups) != 3 {
		t.Fatalf("world pickups = %d, want 3: %+v", len(engine.pickups), engine.pickups)
	}
	seen := make(map[Cell]struct{}, len(engine.pickups))
	for _, pickup := range engine.pickups {
		if pickup.State != PickupAvailable || (pickup.SceneID != 21 && pickup.SceneID != 23) {
			t.Fatalf("invalid scattered pickup: %+v", pickup)
		}
		if _, duplicate := seen[pickup.Cell]; duplicate {
			t.Fatalf("duplicate scattered cell: %+v", pickup.Cell)
		}
		seen[pickup.Cell] = struct{}{}
	}
	observation, err := engine.Observation(config.Participants[1].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Pickups) != 3 {
		t.Fatalf("training observation pickups = %d, want 3", len(observation.Pickups))
	}
}

func TestVerifiedHumanEliminationUsesNativeDropCellsWithoutReroll(t *testing.T) {
	engine := mustEngine(t, testConfig())
	want := []Pickup{
		{SceneID: 21, Cell: Cell{Row: 0, Col: 2}, State: PickupAvailable},
		{SceneID: 25, Cell: Cell{Row: 2, Col: 2}, State: PickupAvailable},
	}
	events, err := engine.ApplyVerifiedElimination(engine.actors[0].PlayerID, engine.actors[1].PlayerID, want)
	if err != nil {
		t.Fatal(err)
	}
	if len(engine.pickups) != len(want) {
		t.Fatalf("verified pickups = %+v, want %+v", engine.pickups, want)
	}
	for index := range want {
		if engine.pickups[index] != want[index] {
			t.Fatalf("verified pickup %d = %+v, want %+v", index, engine.pickups[index], want[index])
		}
	}
	if countEventKind(events, EventPickupDropped) != len(want) {
		t.Fatalf("verified drop events = %+v", events)
	}
}

func TestVerifiedDeathDropReplacesConflictingMirrorPickup(t *testing.T) {
	config := testConfig()
	cell := Cell{Row: 0, Col: 2}
	config.Pickups = []Pickup{{SceneID: SceneBombCapacitySmall, Cell: cell, State: PickupAvailable}}
	engine := mustEngine(t, config)
	want := Pickup{SceneID: 21, Cell: cell, State: PickupAvailable}
	if _, err := engine.ApplyVerifiedElimination(engine.actors[0].PlayerID, engine.actors[1].PlayerID, []Pickup{want}); err != nil {
		t.Fatal(err)
	}
	live := 0
	for _, pickup := range engine.pickups {
		if pickup.Cell == cell && pickup.State != PickupCollected {
			live++
			if pickup != want {
				t.Fatalf("authoritative death drop = %+v, want %+v", pickup, want)
			}
		}
	}
	if live != 1 {
		t.Fatalf("live pickups at death cell = %d: %+v", live, engine.pickups)
	}
}

func TestRuleOneDeathDropsAccumulatedBasicPickupsBeforeHeldActions(t *testing.T) {
	engine := mustEngine(t, testConfig())
	actor := &engine.actors[0]
	actor.State = ActorTrapped
	actor.nativeBasicPickupDelta = [3]int16{12, 2, -1}
	actor.HeldActions[0] = HeldActionSlot{ActionID: 41, Count: 1}

	events, err := engine.ConfirmTrappedDeath(actor.PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[uint32]int)
	for _, event := range events {
		if event.Kind == EventPickupDropped {
			counts[event.SceneID]++
		}
	}
	if counts[SceneBombCapacitySmall] != 10 || counts[SceneBombPowerSmall] != 2 || counts[SceneSpeedSmall] != 0 || counts[21] != 1 {
		t.Fatalf("native death drops = %+v, want scene 1x10, 2x2, 21x1", counts)
	}
	if actor.nativeBasicPickupDelta != ([3]int16{}) {
		t.Fatalf("native basic pickup counters survived elimination: %+v", actor.nativeBasicPickupDelta)
	}
}

func TestPickupCollectionRecordsRawNativeDeathDeltaAtAttributeCap(t *testing.T) {
	config := testConfig()
	config.Participants[0].MaxBombCapacity = config.Participants[0].BombCapacity
	engine := mustEngine(t, config)
	actor := &engine.actors[0]
	pickup := Pickup{SceneID: SceneBombCapacityLarge, Cell: actor.Position.Cell(), State: PickupAvailable}
	before := actor.BombCapacity
	engine.collectPickup(actor, &pickup)
	if actor.BombCapacity != before {
		t.Fatalf("capped capacity changed from %d to %d", before, actor.BombCapacity)
	}
	if got := actor.nativeBasicPickupDelta[0]; got != 8 {
		t.Fatalf("native capacity pickup delta = %d, want raw +8", got)
	}
}

func countEventKind(events []Event, kind EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}
