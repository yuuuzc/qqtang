package battleengine

import "testing"

func TestPublicBehaviorMemoryTracksVisibleShortAndLongHorizonActions(t *testing.T) {
	engine := mustEngine(t, testConfig())
	if _, err := engine.Step([]Action{
		{PlayerID: 1, Move: DirectionRight},
		{PlayerID: 2},
	}); err != nil {
		t.Fatal(err)
	}
	actors := engine.Actors()
	first := actors[0].PublicBehavior
	second := actors[1].PublicBehavior
	if first.Samples != 1 || first.Totals[PublicBehaviorMove] != 1 || first.Totals[PublicBehaviorWait] != 0 {
		t.Fatalf("moving actor memory = %+v", first)
	}
	if second.Samples != 1 || second.Totals[PublicBehaviorWait] != 1 || second.Totals[PublicBehaviorMove] != 0 {
		t.Fatalf("waiting actor memory = %+v", second)
	}
	if first.RecentRate(PublicBehaviorMove) <= 0 || second.RecentRate(PublicBehaviorWait) <= 0 {
		t.Fatalf("recent behavior was not entered: first=%+v second=%+v", first, second)
	}

	observation, err := engine.Observation(2)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Actors[0].PublicBehavior != first {
		t.Fatalf("public behavior projection = %+v, want %+v", observation.Actors[0].PublicBehavior, first)
	}
}

func TestPublicBehaviorEMARecordsRareEventAndDecays(t *testing.T) {
	entered := updatePublicBehaviorEMA(0, true, 20)
	if entered == 0 {
		t.Fatal("rare public event did not enter recent memory")
	}
	decayed := updatePublicBehaviorEMA(entered, false, 20)
	if decayed >= entered {
		t.Fatalf("recent memory did not decay: entered=%d decayed=%d", entered, decayed)
	}
}
