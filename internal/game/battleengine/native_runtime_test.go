package battleengine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"qqtang/internal/game/mapdata"
)

func TestNewRuntimeFromCompetitiveMapBuildsExactNativeBoundary(t *testing.T) {
	entry := testNativeRuntimeMap()
	var policyCalls int
	runtime, err := NewRuntimeFromCompetitiveMap(entry, CompetitiveRuntimeOptions{
		SimulationSeed: 91,
		SpawnSeed:      7,
		ItemSeed:       11,
		SpawnMode:      NativeSpawnFree,
		TickMS:         100,
		Participants: []NativeRuntimeParticipant{
			{PlayerID: 41, RoleID: 1, TeamID: 1, Source: ParticipantHuman},
			{PlayerID: 82, RoleID: 10, TeamID: 2, Source: ParticipantVirtualAI},
		},
		Policies: map[uint16]Policy{82: PolicyFunc(func(observation Observation, legal []Action) (Action, error) {
			policyCalls++
			if observation.PlayerID != 82 || observation.Actors[1].RoleID != 10 {
				t.Fatalf("virtual observation = %+v", observation)
			}
			return Action{PlayerID: 82}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.HumanPlayerIDs(); !reflect.DeepEqual(got, []uint16{41}) {
		t.Fatalf("human IDs = %v", got)
	}
	if got := runtime.VirtualPlayerIDs(); !reflect.DeepEqual(got, []uint16{82}) {
		t.Fatalf("virtual IDs = %v", got)
	}
	snapshot := runtime.EngineSnapshot()
	if snapshot.ElapsedMS() != NativeRoundStartClockMS || snapshot.RoundElapsedMS() != 0 {
		t.Fatalf("native runtime start clocks = wire:%d round:%d", snapshot.ElapsedMS(), snapshot.RoundElapsedMS())
	}
	actors := snapshot.Actors()
	if actors[0].RoleID != 1 || actors[0].BombCapacity != 2 || actors[0].BombPower != 2 || actors[0].SpeedRate != 4 {
		t.Fatalf("role 1 actor = %+v", actors[0])
	}
	if actors[1].RoleID != 10 || actors[1].BombCapacity != 3 || actors[1].BombPower != 4 || actors[1].SpeedRate != 5 {
		t.Fatalf("role 10 actor = %+v", actors[1])
	}
	if snapshot.rules.RoundDurationMS != ProtocolGameTimeMS || snapshot.rules.BombFuseMS != NativeBombFuseMS || snapshot.rules.FlameDurationMS != NativeFlameDurationMS {
		t.Fatalf("native runtime rules = %+v", snapshot.rules)
	}
	arbitrator, err := snapshot.SelectArbitrator(82)
	if err != nil || arbitrator != 41 {
		t.Fatalf("arbitrator = %d, %v", arbitrator, err)
	}
	if _, err = runtime.Step([]Action{{PlayerID: 41}}); err != nil {
		t.Fatal(err)
	}
	if snapshot = runtime.EngineSnapshot(); snapshot.ElapsedMS() != NativeRoundStartClockMS+100 || snapshot.RoundElapsedMS() != 100 {
		t.Fatalf("native runtime advanced clocks = wire:%d round:%d", snapshot.ElapsedMS(), snapshot.RoundElapsedMS())
	}
	if policyCalls != 1 {
		t.Fatalf("policy calls = %d, want 1", policyCalls)
	}
}

func TestNativeSceneDeadlineIncludesReadyGoClock(t *testing.T) {
	config := testConfig()
	config.Rules.StartClockMS = 3_000
	config.Rules.RoundDurationMS = 4_000
	config.Rules.TickMS = 100
	engine := mustEngine(t, config)
	observation, err := engine.Observation(1)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ClockMS != 3_000 || observation.ElapsedMS != 0 || observation.RemainingMS != 1_000 {
		t.Fatalf("initial native observation clock = wire %d elapsed %d remaining %d, want 3000/0/1000", observation.ClockMS, observation.ElapsedMS, observation.RemainingMS)
	}
	for step := 0; step < 9; step++ {
		if _, err = engine.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if engine.Terminal().Ended {
		t.Fatal("native round ended before absolute scene deadline")
	}
	if _, err = engine.Step(nil); err != nil {
		t.Fatal(err)
	}
	outcome := engine.Terminal()
	if !outcome.Ended || !outcome.Draw || !outcome.TimedOut || outcome.EndedAtMS != 1_000 || engine.ElapsedMS() != 4_000 {
		t.Fatalf("native deadline outcome = %+v wire=%d, want draw at playable 1000/wire 4000", outcome, engine.ElapsedMS())
	}
}

func TestNewRuntimeFromCompetitiveMapRejectsInvalidProjection(t *testing.T) {
	entry := testNativeRuntimeMap()
	entry.PlayerLimit = 1
	_, err := NewRuntimeFromCompetitiveMap(entry, CompetitiveRuntimeOptions{
		TickMS: 100,
		Participants: []NativeRuntimeParticipant{
			{PlayerID: 1, RoleID: 1, TeamID: 1},
			{PlayerID: 2, RoleID: 2, TeamID: 2},
		},
	})
	if err == nil {
		t.Fatal("runtime accepted participants above the native map limit")
	}
	entry.PlayerLimit = 2
	_, err = NewRuntimeFromCompetitiveMap(entry, CompetitiveRuntimeOptions{
		TickMS: 100,
		Participants: []NativeRuntimeParticipant{
			{PlayerID: 1, RoleID: 23, TeamID: 1},
			{PlayerID: 2, RoleID: 2, TeamID: 2},
		},
	})
	if err == nil {
		t.Fatal("runtime accepted the random-role placeholder as a combat model")
	}
}

func TestShippedOrdinaryMapsBuildNativeRuntime(t *testing.T) {
	root := filepath.Join("..", "..", "..", "runtime", "client-patched")
	if _, err := os.Stat(filepath.Join(root, "map", "mapDesc.py")); os.IsNotExist(err) {
		t.Skip("verified runtime client is not present")
	}
	catalog, err := mapdata.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, mapID := range catalog.AllCompetitiveIDs() {
		entry, ok := catalog.CompetitiveMap(mapID)
		if !ok || entry.NativeRule != 1 || entry.Rule != mapdata.CompetitiveRuleOrdinary || entry.PlayerLimit < 2 {
			continue
		}
		if err = ValidateNativeRuntimeMap(entry); err != nil {
			t.Fatalf("ordinary map %d remains outside the closed native pickup set: %v", mapID, err)
		}
		_, err = NewRuntimeFromCompetitiveMap(entry, CompetitiveRuntimeOptions{
			SpawnSeed: uint32(mapID), ItemSeed: uint32(mapID) ^ 0x5A5A5A5A, TickMS: 100,
			Participants: []NativeRuntimeParticipant{
				{PlayerID: 1, RoleID: 1, TeamID: 1, Source: ParticipantHuman},
				{PlayerID: 2, RoleID: 2, TeamID: 2, Source: ParticipantHuman},
			},
		})
		if err != nil {
			t.Fatalf("ordinary map %d runtime: %v", mapID, err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("shipped catalog contains no supported ordinary map runtime")
	}
}

func testNativeRuntimeMap() mapdata.CompetitiveMap {
	return mapdata.CompetitiveMap{
		ID: 7001, PlayerLimit: 2, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		Battlefield: mapdata.CompetitiveBattlefield{
			Width: 3, Height: 1,
			Cells: []mapdata.CompetitiveBattleCell{
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
			},
		},
		SpawnGroupA: []mapdata.CompetitiveCell{{Row: 0, Col: 0}},
		SpawnGroupB: []mapdata.CompetitiveCell{{Row: 0, Col: 2}},
	}
}
