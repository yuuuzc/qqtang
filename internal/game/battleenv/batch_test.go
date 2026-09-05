package battleenv

import (
	"math"
	"reflect"
	"testing"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/mapdata"
)

func requireRewardNear(t *testing.T, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1e-6 {
		t.Fatalf("reward=%v, want %v", got, want)
	}
}

func TestBatchResetAndObservationAreDeterministic(t *testing.T) {
	config := testBatchConfig(3)
	first, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	firstObservation, err := first.Observe()
	if err != nil {
		t.Fatal(err)
	}
	secondObservation, err := second.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.MapIDs(), second.MapIDs()) || !reflect.DeepEqual(firstObservation, secondObservation) {
		t.Fatal("same batch seed did not produce identical reset tensors")
	}
	if got, want := len(firstObservation.Spatial), 3*2*SpatialChannels*3*5; got != want {
		t.Fatalf("spatial length = %d, want %d", got, want)
	}
	if got, want := len(firstObservation.Scalars), 3*2*ScalarFeatures; got != want {
		t.Fatalf("scalar length = %d, want %d", got, want)
	}
	for envIndex := 0; envIndex < 3; envIndex++ {
		for actorIndex := 0; actorIndex < 2; actorIndex++ {
			base := (envIndex*2 + actorIndex) * int(battleengine.DiscreteActionCount)
			if firstObservation.Legal[base+int(battleengine.ActionWait)] != 1 || firstObservation.Active[envIndex*2+actorIndex] != 1 {
				t.Fatalf("environment %d actor %d lacks active wait action", envIndex, actorIndex)
			}
		}
	}
}

func TestDuelCurriculumBuildsConnectedDevelopedEndgameState(t *testing.T) {
	grid := battleengine.Grid{Width: 5, Height: 3, Cells: make([]battleengine.Tile, 15)}
	for index := range grid.Cells {
		grid.Cells[index] = battleengine.Tile{Kind: battleengine.CellOpen, FlamePassable: true}
	}
	grid.Cells[7] = battleengine.Tile{Kind: battleengine.CellBreakable, Durability: 1}
	config := battleengine.Config{
		Grid: grid,
		Participants: []battleengine.Participant{
			{PlayerID: 1, TeamID: 1, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
			{PlayerID: 2, TeamID: 2, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
		},
		Pickups: []battleengine.Pickup{{SceneID: 1}},
	}

	if err := applyDuelCurriculum(&config, 123); err != nil {
		t.Fatal(err)
	}
	if tile, _ := config.Grid.Cell(battleengine.Cell{Row: 1, Col: 2}); tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
		t.Fatalf("breakable terrain survived duel curriculum: %+v", tile)
	}
	if len(config.Pickups) != 0 {
		t.Fatalf("developed duel retained opening pickups: %+v", config.Pickups)
	}
	first, second := config.Participants[0], config.Participants[1]
	distance := absInt(int(first.Spawn.Row-second.Spawn.Row)) + absInt(int(first.Spawn.Col-second.Spawn.Col))
	if distance < 3 || distance > 6 {
		t.Fatalf("duel spawn distance=%d, actors=%+v/%+v", distance, first, second)
	}
	for _, participant := range config.Participants {
		if participant.BombCapacity < 2 || participant.BombPower < 2 || participant.SpeedRate <= 2 {
			t.Fatalf("duel attributes were not developed: %+v", participant)
		}
		if participant.BombCapacity > participant.MaxBombCapacity || participant.BombPower > participant.MaxBombPower || participant.SpeedRate > participant.MaxSpeedRate {
			t.Fatalf("duel attributes exceeded native maxima: %+v", participant)
		}
	}
}

func TestBombEscapeCurriculumSpawnsHaveShortSafeRoutes(t *testing.T) {
	grid := battleengine.Grid{Width: 7, Height: 5, Cells: make([]battleengine.Tile, 35)}
	for index := range grid.Cells {
		grid.Cells[index] = battleengine.Tile{Kind: battleengine.CellOpen, FlamePassable: true}
	}
	config := battleengine.Config{
		Grid: grid,
		Participants: []battleengine.Participant{
			{PlayerID: 1, TeamID: 1, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
			{PlayerID: 2, TeamID: 2, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
		},
	}
	for seed := uint64(1); seed <= 64; seed++ {
		candidate := config
		candidate.Participants = append([]battleengine.Participant(nil), config.Participants...)
		if err := applyBombEscapeCurriculum(&candidate, seed, 1); err != nil {
			t.Fatal(err)
		}
		for _, participant := range candidate.Participants {
			if participant.BombCapacity != 1 {
				t.Fatalf("seed %d retained multi-bomb capacity: %+v", seed, participant)
			}
			if !bombEscapeRouteExists(candidate.Grid, participant.Spawn, participant.BombPower, bombEscapeMaximumPathCells) {
				t.Fatalf("seed %d produced impossible escape spawn: %+v", seed, participant)
			}
		}
	}
}

func TestBombEscapeRouteRejectsClosedStraightCorridor(t *testing.T) {
	grid := battleengine.Grid{Width: 5, Height: 1, Cells: make([]battleengine.Tile, 5)}
	for index := range grid.Cells {
		grid.Cells[index] = battleengine.Tile{Kind: battleengine.CellOpen, FlamePassable: true}
	}
	if bombEscapeRouteExists(grid, battleengine.Cell{Col: 2}, 4, bombEscapeMaximumPathCells) {
		t.Fatal("straight corridor entirely inside the blast was considered escapable")
	}
}

func TestBlockadeCurriculumUsesConnectedLimitedExitSpawns(t *testing.T) {
	grid := battleengine.Grid{Width: 5, Height: 5, Cells: make([]battleengine.Tile, 25)}
	for index := range grid.Cells {
		grid.Cells[index] = battleengine.Tile{Kind: battleengine.CellOpen, FlamePassable: true}
	}
	config := battleengine.Config{
		Grid: grid,
		Participants: []battleengine.Participant{
			{PlayerID: 1, TeamID: 1, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
			{PlayerID: 2, TeamID: 2, BombCapacity: 1, MaxBombCapacity: 4, BombPower: 1, MaxBombPower: 4, SpeedRate: 2, MaxSpeedRate: 6},
		},
	}
	for seed := uint64(1); seed <= 64; seed++ {
		candidate := config
		candidate.Participants = append([]battleengine.Participant(nil), config.Participants...)
		if err := applyBlockadeCurriculum(&candidate, seed); err != nil {
			t.Fatal(err)
		}
		first, second := candidate.Participants[0], candidate.Participants[1]
		if openNeighborCount(candidate.Grid, first.Spawn) > 2 || openNeighborCount(candidate.Grid, second.Spawn) > 2 {
			t.Fatalf("seed %d produced open-arena spawns: %+v/%+v", seed, first.Spawn, second.Spawn)
		}
		distance, connected := openPathDistance(candidate.Grid, first.Spawn, second.Spawn, 6)
		if !connected || distance < 2 {
			t.Fatalf("seed %d produced disconnected/distant spawns: distance=%d connected=%v", seed, distance, connected)
		}
	}
}

func TestBatchMarksSampledDuelCurriculum(t *testing.T) {
	config := testBatchConfig(1)
	config.DuelCurriculumPermille = 1000
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.Metrics()[0].DuelCurriculum {
		t.Fatal("forced duel curriculum reset was not reported")
	}
}

func TestDuelCurriculumCanFixNativeBombCapacity(t *testing.T) {
	config := testBatchConfig(1)
	config.DuelCurriculumPermille = 1000
	config.DuelCurriculumBombCapacity = 2
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range batch.episodes[0].engine.Actors() {
		if actor.BombCapacity != 2 {
			t.Fatalf("duel course capacity=%d, want 2", actor.BombCapacity)
		}
	}
}

func TestBatchRejectsInvalidDuelCurriculumProbability(t *testing.T) {
	config := testBatchConfig(1)
	config.DuelCurriculumPermille = 1001
	if _, err := NewBatch(config); err == nil {
		t.Fatal("invalid duel curriculum probability was accepted")
	}
}

func TestBatchMarksSampledBombEscapeCurriculum(t *testing.T) {
	config := testBatchConfig(1)
	config.BombEscapeCurriculumPermille = 1000
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	metrics := batch.Metrics()[0]
	if !metrics.BombEscapeCurriculum || metrics.DuelCurriculum {
		t.Fatalf("forced bomb escape curriculum reset was not isolated: %+v", metrics)
	}
}

func TestBatchSeedsSampledChainFinisherCurriculum(t *testing.T) {
	config := testBatchConfig(1)
	config.ChainFinisherCurriculumPermille = 1000
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	metrics := batch.Metrics()[0]
	if !metrics.ChainFinisherCurriculum || metrics.DuelCurriculum || metrics.BlockadeCurriculum || metrics.BombEscapeCurriculum {
		t.Fatalf("forced chain finisher reset was not isolated: %+v", metrics)
	}
	engine := batch.episodes[0].engine
	bombs := engine.Bombs()
	if len(bombs) != 1 {
		t.Fatalf("chain finisher root bubbles=%d, want 1: %+v", len(bombs), bombs)
	}
	remaining := bombs[0].EffectiveExplodeAtMS() - engine.ElapsedMS()
	if remaining < 600 || remaining > 1000 {
		t.Fatalf("chain finisher root remaining=%dms, want 600..1000", remaining)
	}
	owner := -1
	for index, actor := range engine.Actors() {
		if actor.PlayerID == bombs[0].OwnerID {
			owner = index
			break
		}
	}
	if owner < 0 {
		t.Fatalf("chain finisher root owner %d is absent", bombs[0].OwnerID)
	}
}

func TestBatchRejectsOverlappingCurriculumProbability(t *testing.T) {
	config := testBatchConfig(1)
	config.DuelCurriculumPermille = 600
	config.BombEscapeCurriculumPermille = 500
	if _, err := NewBatch(config); err == nil {
		t.Fatal("combined curriculum probability above 100% was accepted")
	}
}

func TestSafeBombEscapeOwnersRequirePostBlastActiveActor(t *testing.T) {
	actors := []battleengine.Actor{
		{Participant: battleengine.Participant{PlayerID: 1, TeamID: 1}, State: battleengine.ActorActive},
		{Participant: battleengine.Participant{PlayerID: 2, TeamID: 2}, State: battleengine.ActorTrapped},
	}
	events := []battleengine.Event{{Kind: battleengine.EventBombExploded, PlayerID: 1}}
	if owners := safeBombEscapeOwners(events, actors); !reflect.DeepEqual(owners, []uint16{1}) {
		t.Fatalf("safe owner was not credited: %v", owners)
	}
	events = []battleengine.Event{{Kind: battleengine.EventBombExploded, PlayerID: 2}}
	if owners := safeBombEscapeOwners(events, actors); len(owners) != 0 {
		t.Fatalf("trapped owner received escape credit: %v", owners)
	}
	actors[1].State = battleengine.ActorActive
	events = []battleengine.Event{
		{Kind: battleengine.EventBombExploded, PlayerID: 1},
		{Kind: battleengine.EventBombExploded, PlayerID: 2},
		{Kind: battleengine.EventBombExploded, PlayerID: 1},
	}
	if owners := safeBombEscapeOwners(events, actors); !reflect.DeepEqual(owners, []uint16{1, 2}) {
		t.Fatalf("simultaneous safe owners were not returned once each: %v", owners)
	}
}

func TestBatchDecisionHoldsMovementButPulsesPlacementOnce(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	before := batch.episodes[0].engine.Actors()[0]
	result, err := batch.Step([][]battleengine.ActionID{{battleengine.ActionMoveRightAndPlaceBomb, battleengine.ActionWait}})
	if err != nil {
		t.Fatal(err)
	}
	after := batch.episodes[0].engine.Actors()[0]
	if after.Position.X <= before.Position.X {
		t.Fatalf("held decision did not move actor: before=%+v after=%+v", before.Position, after.Position)
	}
	if got := len(batch.episodes[0].engine.Bombs()); got != 1 {
		t.Fatalf("placement pulse repeated across decision ticks: bombs=%d", got)
	}
	if result.Dones[0] != 0 || result.Observation.EnvCount != 1 || batch.episodes[0].metrics.Ticks != 5 {
		t.Fatalf("unexpected decision result: done=%d envs=%d metrics=%+v", result.Dones[0], result.Observation.EnvCount, batch.episodes[0].metrics)
	}
}

func TestBatchRejectsItemFieldAndSpecialRuleMaps(t *testing.T) {
	config := testBatchConfig(1)
	config.Maps[0].RequiredItemField = 1
	if _, err := NewBatch(config); err == nil {
		t.Fatal("item-field map entered restricted training batch")
	}
	config = testBatchConfig(1)
	config.Maps[0].NativeRule = 2
	config.Maps[0].Rule = mapdata.CompetitiveRuleKickBomb
	if _, err := NewBatch(config); err == nil {
		t.Fatal("special-rule map entered restricted training batch")
	}
}

func TestBatchAssignsEqualSizedMultiTeams(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamCount = 4
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	actors := batch.episodes[0].engine.Actors()
	for index, actor := range actors {
		if want := byte(index + 1); actor.TeamID != want {
			t.Fatalf("actor %d team = %d, want %d", index, actor.TeamID, want)
		}
	}
}

func TestBatchAcceptsExplicitUnevenTeams(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamLayouts = [][]uint8{{1, 2, 2, 2}}
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	actors := batch.episodes[0].engine.Actors()
	for index, want := range []byte{1, 2, 2, 2} {
		if actors[index].TeamID != want {
			t.Fatalf("actor %d team=%d, want %d", index, actors[index].TeamID, want)
		}
	}
	observation, err := batch.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observation.TeamIDs, []uint8{1, 2, 2, 2}) {
		t.Fatalf("tensor team IDs=%v", observation.TeamIDs)
	}
}

func TestBatchMixesParticipantCountsWithInactivePadding(t *testing.T) {
	config := testBatchConfig(16)
	config.ParticipantCount = 4
	config.ParticipantCounts = []int{2, 4}
	config.TeamLayouts = [][]uint8{{1, 2}, {1, 1, 2, 2}, {1, 2, 3, 4}}
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := batch.Observe()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	actions := make([][]battleengine.ActionID, len(batch.episodes))
	for envIndex, episode := range batch.episodes {
		count := len(episode.playerIDs)
		seen[count] = true
		actions[envIndex] = make([]battleengine.ActionID, config.ParticipantCount)
		for actorIndex := 0; actorIndex < config.ParticipantCount; actorIndex++ {
			active := observation.Active[envIndex*config.ParticipantCount+actorIndex]
			teamID := observation.TeamIDs[envIndex*config.ParticipantCount+actorIndex]
			if actorIndex < count && (active != 1 || teamID == 0) {
				t.Fatalf("environment %d real actor %d active/team=%d/%d", envIndex, actorIndex, active, teamID)
			}
			if actorIndex >= count && (active != 0 || teamID != 0) {
				t.Fatalf("environment %d padded actor %d active/team=%d/%d", envIndex, actorIndex, active, teamID)
			}
		}
	}
	if !seen[2] || !seen[4] {
		t.Fatalf("deterministic mixed reset counts=%v, want both 2 and 4", seen)
	}
	if _, err := batch.Step(actions); err != nil {
		t.Fatalf("padded action tensor was rejected: %v", err)
	}
}

func TestBatchUsesNativeTeamSpawnsOnlyForBalancedTwoTeamLayouts(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamLayouts = [][]uint8{{1, 1, 2, 2}}
	config.TeamSpawnPermille = 1000
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	actors := batch.episodes[0].engine.Actors()
	for _, actor := range actors {
		if actor.TeamID == 1 && actor.Position.Cell().Row != 0 {
			t.Fatalf("team 1 actor spawned outside native group A: %+v", actor)
		}
		if actor.TeamID == 2 && actor.Position.Cell().Row != 2 {
			t.Fatalf("team 2 actor spawned outside native group B: %+v", actor)
		}
	}

	config.TeamLayouts = [][]uint8{{1, 2, 2, 2}}
	if _, err = NewBatch(config); err != nil {
		t.Fatalf("uneven layout did not fall back to native free spawn: %v", err)
	}
}

func TestBatchRejectsMalformedExplicitTeamLayout(t *testing.T) {
	config := testBatchConfig(1)
	config.TeamLayouts = [][]uint8{{1, 3}}
	if _, err := NewBatch(config); err == nil {
		t.Fatal("non-contiguous explicit TeamIDs were accepted")
	}
}

func TestEventRewardsDoNotCreditSelfDamage(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 2)
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventBombPlaced, PlayerID: 1},
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 1},
	}, rewards)
	if got, want := rewards[0], rewardBombPlaced-rewardTrap; got != want {
		t.Fatalf("self-trap reward = %v, want %v", got, want)
	}
	rewards = make([]float32, 2)
	batch.accumulateEvents(0, []battleengine.Event{{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 2}}, rewards)
	if rewards[0] != rewardTrap || rewards[1] != -rewardTrap {
		t.Fatalf("enemy trap rewards = %v, want [%v %v]", rewards, rewardTrap, -rewardTrap)
	}
}

func TestEventRewardsPenalizeFriendlyFire(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamCount = 2
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 4)
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 2},
	}, rewards)
	teamScale := float32(0.5)
	if rewards[0] != -rewardTrap*teamScale*rewardFriendlyFireFactor || rewards[1] != -rewardTrap*teamScale {
		t.Fatalf("friendly trap rewards = %v, want mild attacker and full target penalties", rewards)
	}
	for _, value := range rewards[2:] {
		if value != 0 {
			t.Fatalf("unrelated opponent received friendly-fire reward: %v", rewards)
		}
	}
}

func TestEventRewardsNormalizeUnevenFreeRoomTeams(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamCount = 3
	config.TeamLayouts = [][]uint8{{1, 2, 2, 3}}
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 4)
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 2},
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 3},
	}, rewards)
	if got, want := rewards[0], rewardTrap; got != want {
		t.Fatalf("two members of one free-room team paid %v, want one team-sized %v", got, want)
	}
	if rewards[1] != -rewardTrap/2 || rewards[2] != -rewardTrap/2 || rewards[3] != 0 {
		t.Fatalf("uneven free-room target scaling=%v", rewards)
	}

	rewards = make([]float32, 4)
	batch.accumulateEvents(0, []battleengine.Event{{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 4}}, rewards)
	if rewards[0] != rewardTrap || rewards[3] != -rewardTrap {
		t.Fatalf("singleton free-room team should retain 1v1 impact: %v", rewards)
	}
}

func TestEventRewardsDoNotPayFriendlyTrapRescueLoops(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamCount = 2
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 4)
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 2},
		{Kind: battleengine.EventActorRescued, PlayerID: 1, TargetID: 2},
	}, rewards)
	for _, value := range rewards {
		if value != 0 {
			t.Fatalf("friendly trap/rescue loop retained reward: %v", rewards)
		}
	}
}

func TestEventRewardsRollbackEnemyTrapAndBoundRescueCredit(t *testing.T) {
	config := testBatchConfig(1)
	config.ParticipantCount = 4
	config.TeamCount = 2
	config.Maps[0].PlayerLimit = 4
	config.Maps[0].SpawnGroupA = []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 4}}
	config.Maps[0].SpawnGroupB = []mapdata.CompetitiveCell{{Row: 2, Col: 0}, {Row: 2, Col: 4}}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	// Player 2's adjacent bubble closes one immediate route while player 1
	// supplies the trapping flame. The assist pool is credited once and then
	// rolled back with the direct trap when player 4 rescues player 3.
	targetCell := battleengine.Cell{Row: 1, Col: 2}
	if err := batch.episodes[0].engine.ApplyVerifiedMovementCheckpoint(3, battleengine.PositionAtCellCenter(targetCell)); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.episodes[0].engine.ApplyVerifiedBombPlacement(2, battleengine.Cell{Row: 1, Col: 1}, 2, 0); err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 4)
	batch.accumulateEvents(0, []battleengine.Event{{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 3}}, rewards)
	teamScale := float32(0.5)
	if rewards[0] != rewardTrap*teamScale || rewards[1] != rewardTrapAssistTotal*teamScale || rewards[2] != -rewardTrap*teamScale || rewards[3] != 0 {
		t.Fatalf("direct/assist trap rewards=%v", rewards)
	}
	batch.accumulateEvents(0, []battleengine.Event{{Kind: battleengine.EventActorRescued, PlayerID: 4, TargetID: 3}}, rewards)
	if rewards[0] != 0 || rewards[1] != 0 || rewards[2] != 0 || rewards[3] != rewardRescue*teamScale {
		t.Fatalf("rescued enemy trap did not roll back causal credit: %v", rewards)
	}
	// A later rescue of the same target still rolls combat credit back and
	// receives only half of the previous rescue credit.
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventActorTrapped, PlayerID: 1, TargetID: 3},
		{Kind: battleengine.EventActorRescued, PlayerID: 4, TargetID: 3},
	}, rewards)
	if rewards[0] != 0 || rewards[1] != 0 || rewards[2] != 0 || rewards[3] != (rewardRescue+rewardRescue/2)*teamScale {
		t.Fatalf("repeat rescue did not decay geometrically: %v", rewards)
	}
}

func TestEventRewardsCreditUsefulActiveItems(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 2)
	batch.accumulateEvents(0, []battleengine.Event{
		{Kind: battleengine.EventFieldObjectTriggered, PlayerID: 1, TargetID: 2, ActionID: 43},
		{Kind: battleengine.EventActorRescued, PlayerID: 1, TargetID: 1, ActionID: 63},
	}, rewards)
	if got, want := rewards[0], rewardMovementDebuff+rewardRescue; got != want {
		t.Fatalf("active-item owner reward = %v, want %v", got, want)
	}
	if got, want := rewards[1], -rewardMovementDebuff; got != want {
		t.Fatalf("debuff target reward = %v, want %v", got, want)
	}
}

func TestEventRewardsTreatHitTransformationAsExtraLife(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 2)
	batch.accumulateEvents(0, []battleengine.Event{{
		Kind: battleengine.EventActorTransformationEnded, PlayerID: 2, TargetID: 1,
		TransformationEnd: battleengine.TransformationEndHit,
	}}, rewards)
	if rewards[0] != rewardTransformationBroken || rewards[1] != -rewardTransformationBroken {
		t.Fatalf("enemy transformation break rewards = %v", rewards)
	}
	rewards = make([]float32, 2)
	batch.accumulateEvents(0, []battleengine.Event{{
		Kind: battleengine.EventActorTransformationEnded, PlayerID: 2,
		TransformationEnd: battleengine.TransformationEndExpired,
	}}, rewards)
	if rewards[0] != 0 || rewards[1] != 0 {
		t.Fatalf("natural transformation expiry changed rewards: %v", rewards)
	}
}

func TestTerminalRewardPrefersFastWinsAndPenalizesFastLosses(t *testing.T) {
	duel := materialByTeam([]battleengine.Actor{
		{Participant: battleengine.Participant{TeamID: 1}},
		{Participant: battleengine.Participant{TeamID: 2}},
	})
	fast := battleengine.Outcome{Ended: true, WinnerTeamID: 1, EndedAtMS: 60_000}
	late := battleengine.Outcome{Ended: true, WinnerTeamID: 1, EndedAtMS: 220_000}
	if terminalOutcomeReward(1, fast, duel) <= terminalOutcomeReward(1, late, duel) {
		t.Fatalf("fast win reward=%v, late=%v", terminalOutcomeReward(1, fast, duel), terminalOutcomeReward(1, late, duel))
	}
	if terminalOutcomeReward(2, fast, duel) >= terminalOutcomeReward(2, late, duel) {
		t.Fatalf("fast loss reward=%v, late=%v", terminalOutcomeReward(2, fast, duel), terminalOutcomeReward(2, late, duel))
	}
	draw := battleengine.Outcome{Ended: true, Draw: true, TimedOut: true, EndedAtMS: battleengine.StandardRoundTimeMS}
	if got := terminalOutcomeReward(1, draw, duel); got != rewardDraw {
		t.Fatalf("draw reward=%v, want %v", got, rewardDraw)
	}
	earlyDraw := battleengine.Outcome{Ended: true, Draw: true, EndedAtMS: 60_000}
	if got := terminalOutcomeReward(1, earlyDraw, duel); got != rewardDraw {
		t.Fatalf("non-timeout draw reward=%v, want %v", got, rewardDraw)
	}
	lateLoss := battleengine.Outcome{Ended: true, WinnerTeamID: 2, EndedAtMS: battleengine.StandardRoundTimeMS}
	immediateLoss := battleengine.Outcome{Ended: true, WinnerTeamID: 2, EndedAtMS: 0}
	if terminalOutcomeReward(1, draw, duel) <= terminalOutcomeReward(1, lateLoss, duel) {
		t.Fatalf("timeout draw reward=%v must exceed late loss=%v", terminalOutcomeReward(1, draw, duel), terminalOutcomeReward(1, lateLoss, duel))
	}
	if terminalOutcomeReward(1, draw, duel) <= terminalOutcomeReward(1, immediateLoss, duel) {
		t.Fatalf("timeout draw reward=%v must exceed immediate loss=%v", terminalOutcomeReward(1, draw, duel), terminalOutcomeReward(1, immediateLoss, duel))
	}
}

func TestTimeoutDrawRewardUsesInitialAndLiveTeamMaterial(t *testing.T) {
	actors := make([]battleengine.Actor, 0, 8)
	actors = append(actors, battleengine.Actor{Participant: battleengine.Participant{TeamID: 1}})
	for range 7 {
		actors = append(actors, battleengine.Actor{Participant: battleengine.Participant{TeamID: 2}})
	}
	material := materialByTeam(actors)
	requireRewardNear(t, timeoutDrawReward(1, material), 0.96)
	requireRewardNear(t, timeoutDrawReward(2, material), -0.96)
	for index := 1; index < 7; index++ {
		actors[index].State = battleengine.ActorEliminated
	}
	material = materialByTeam(actors)
	requireRewardNear(t, timeoutDrawReward(1, material), 1.71)
	requireRewardNear(t, timeoutDrawReward(2, material), -1.71)
}

func TestTerminalOutcomeRewardNormalizesUnevenTeamDifficulty(t *testing.T) {
	actors := make([]battleengine.Actor, 0, 8)
	actors = append(actors, battleengine.Actor{Participant: battleengine.Participant{TeamID: 1}})
	for range 7 {
		actors = append(actors, battleengine.Actor{Participant: battleengine.Participant{TeamID: 2}})
	}
	material := materialByTeam(actors)
	hardWin := battleengine.Outcome{
		Ended: true, WinnerTeamID: 1, EndedAtMS: battleengine.StandardRoundTimeMS,
	}
	easyWin := battleengine.Outcome{
		Ended: true, WinnerTeamID: 2, EndedAtMS: battleengine.StandardRoundTimeMS,
	}
	requireRewardNear(t, terminalOutcomeReward(1, hardWin, material), 1.96)
	requireRewardNear(t, terminalOutcomeReward(2, hardWin, material), -1.96)
	requireRewardNear(t, terminalOutcomeReward(2, easyWin, material), 0.04)
	requireRewardNear(t, terminalOutcomeReward(1, easyWin, material), -0.04)
	nonTimeoutDraw := battleengine.Outcome{Ended: true, Draw: true}
	requireRewardNear(t, terminalOutcomeReward(1, nonTimeoutDraw, material), 0.96)
	requireRewardNear(t, terminalOutcomeReward(2, nonTimeoutDraw, material), -0.96)
}

func TestOutcomePriorDistinguishesCoalitionFromEightSoloTeams(t *testing.T) {
	coalitionActors := []battleengine.Actor{{Participant: battleengine.Participant{TeamID: 1}}}
	for range 7 {
		coalitionActors = append(coalitionActors, battleengine.Actor{Participant: battleengine.Participant{TeamID: 2}})
	}
	soloActors := make([]battleengine.Actor, 0, 8)
	for teamID := byte(1); teamID <= 8; teamID++ {
		soloActors = append(soloActors, battleengine.Actor{Participant: battleengine.Participant{TeamID: teamID}})
	}
	coalitionWin := initialOutcomeReward(1, 1, materialByTeam(coalitionActors))
	soloWin := initialOutcomeReward(1, 1, materialByTeam(soloActors))
	requireRewardNear(t, coalitionWin, 1.96)
	requireRewardNear(t, soloWin, 1)
	if coalitionWin <= soloWin {
		t.Fatalf("coalition upset reward=%v must exceed eight-way win=%v", coalitionWin, soloWin)
	}
}

func TestTimeoutDrawRewardMeasuresEqualTeamMaterialSwing(t *testing.T) {
	actors := []battleengine.Actor{
		{Participant: battleengine.Participant{TeamID: 1}},
		{Participant: battleengine.Participant{TeamID: 1}, State: battleengine.ActorEliminated},
		{Participant: battleengine.Participant{TeamID: 2}},
		{Participant: battleengine.Participant{TeamID: 2}},
	}
	material := materialByTeam(actors)
	requireRewardNear(t, timeoutDrawReward(1, material), -1.0/3.0)
	requireRewardNear(t, timeoutDrawReward(2, material), 1.0/3.0)
}

func TestThreeTeamOutcomeNormalizationIsZeroSum(t *testing.T) {
	actors := []battleengine.Actor{
		{Participant: battleengine.Participant{TeamID: 1}},
		{Participant: battleengine.Participant{TeamID: 2}},
		{Participant: battleengine.Participant{TeamID: 3}},
	}
	material := materialByTeam(actors)
	outcome := battleengine.Outcome{
		Ended: true, WinnerTeamID: 1, EndedAtMS: battleengine.StandardRoundTimeMS,
	}
	rewards := []float32{
		terminalOutcomeReward(1, outcome, material),
		terminalOutcomeReward(2, outcome, material),
		terminalOutcomeReward(3, outcome, material),
	}
	requireRewardNear(t, rewards[0], 1)
	requireRewardNear(t, rewards[1], -0.5)
	requireRewardNear(t, rewards[2], -0.5)
	if math.Abs(float64(rewards[0]+rewards[1]+rewards[2])) > 1e-6 {
		t.Fatalf("three-team rewards are not zero sum: %v", rewards)
	}
}

func TestLateStallResponsibilityExemptsOutnumberedTeam(t *testing.T) {
	actors := []battleengine.Actor{
		{Participant: battleengine.Participant{TeamID: 1}},
		{Participant: battleengine.Participant{TeamID: 2}},
		{Participant: battleengine.Participant{TeamID: 2}},
	}
	if got := lateStallResponsibility(1, actors); got != 0 {
		t.Fatalf("outnumbered responsibility=%v, want 0", got)
	}
	if got := lateStallResponsibility(2, actors); got != 4.0/3.0 {
		t.Fatalf("advantaged responsibility=%v, want %v", got, float32(4.0/3.0))
	}
}

func TestEnemyApproachPotentialDoesNotRewardLoops(t *testing.T) {
	if got := enemyApproachReward(rewardDevelopmentEndMS-1, 5, 4); got != 0 {
		t.Fatalf("development-phase approach reward = %v, want 0", got)
	}
	if got := enemyApproachReward(rewardDevelopmentEndMS, 5, 4); got <= 0 {
		t.Fatalf("approach reward = %v", got)
	}
	forward := enemyApproachReward(rewardDevelopmentEndMS, 5, 4)
	backward := enemyApproachReward(rewardDevelopmentEndMS, 4, 5)
	if forward+backward != 0 {
		t.Fatalf("round-trip potential reward = %v", forward+backward)
	}
}

func TestDevelopmentProgressBonusEndsAtThirtySeconds(t *testing.T) {
	if got, want := developmentProgressReward(rewardDevelopmentEndMS-1, rewardWallDestroyed, rewardDevelopmentWallBonus), float32(0.03); got != want {
		t.Fatalf("opening wall reward = %v, want %v", got, want)
	}
	if got, want := developmentProgressReward(rewardDevelopmentEndMS-1, rewardPickup, rewardDevelopmentPickupBonus), float32(0.04); got != want {
		t.Fatalf("opening pickup reward = %v, want %v", got, want)
	}
	if got := developmentProgressReward(rewardDevelopmentEndMS, rewardPickup, rewardDevelopmentPickupBonus); got != rewardPickup {
		t.Fatalf("midgame pickup reward = %v, want %v", got, rewardPickup)
	}
}

func TestLateStallPenaltyStartsOnlyAfterNinetySecondsAndProgressGrace(t *testing.T) {
	if got := lateStallPenalty(rewardLateStallStartMS-1, 0, 20); got != 0 {
		t.Fatalf("first-half stall penalty=%v, want 0", got)
	}
	lastProgress := rewardLateStallStartMS - 1_000
	if got := lateStallPenalty(rewardLateStallStartMS+rewardLateStallProgressGraceMS-1_000, lastProgress, 20); got != 0 {
		t.Fatalf("progress grace penalty=%v, want 0", got)
	}
	first := lateStallPenalty(rewardLateStallStartMS+rewardLateStallProgressGraceMS, 0, 20)
	if first >= 0 {
		t.Fatalf("late stall penalty=%v, want negative", first)
	}
	later := lateStallPenalty(rewardRoundDurationMS-1, 0, 20)
	if later >= first {
		t.Fatalf("late stall penalty did not escalate: first=%v later=%v", first, later)
	}
}

func TestNativeBubblePassesAreMeasuredButNotPenalized(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 2)
	events := make([]battleengine.Event, 4)
	for index := range events {
		events[index] = battleengine.Event{Kind: battleengine.EventNativePassStarted, PlayerID: 1, TimeMS: uint32(index * 1_000)}
	}
	batch.accumulateEvents(0, events, rewards)
	if rewards[0] != 0 {
		t.Fatalf("native bubble passes reward=%v, want 0", rewards[0])
	}
	if got := batch.episodes[0].metrics.Agents[0].NativePasses; got != 4 {
		t.Fatalf("native pass metric=%d, want 4", got)
	}
}

func TestBlockedCellCampingHasGraceThenCostsTime(t *testing.T) {
	config := testBatchConfig(1)
	config.Maps[0].Battlefield.Cells[1*5+1] = mapdata.CompetitiveBattleCell{Collision: mapdata.CompetitiveCellSolid}
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.episodes[0].engine.ApplyVerifiedMovementCheckpoint(1, battleengine.PositionAtCellCenter(battleengine.Cell{Row: 1, Col: 1})); err != nil {
		t.Fatal(err)
	}
	rewards := make([]float32, 2)
	graceTicks := (rewardBlockedCellGraceMS + config.TickMS - 1) / config.TickMS
	for range graceTicks {
		batch.accumulateBlockedCellBehavior(0, rewards)
	}
	if rewards[0] != 0 {
		t.Fatalf("blocked-cell grace reward=%v, want 0", rewards[0])
	}
	batch.accumulateBlockedCellBehavior(0, rewards)
	want := -rewardBlockedCellPenaltyPerSecond * float32(config.TickMS) / 1_000
	if rewards[0] != want {
		t.Fatalf("blocked-cell post-grace reward=%v, want %v", rewards[0], want)
	}
	if got := batch.episodes[0].metrics.Agents[0].BlockedTicks; got != graceTicks+1 {
		t.Fatalf("blocked-cell ticks=%d, want %d", got, graceTicks+1)
	}
	if err := batch.episodes[0].engine.ApplyVerifiedMovementCheckpoint(1, battleengine.PositionAtCellCenter(battleengine.Cell{Row: 1, Col: 0})); err != nil {
		t.Fatal(err)
	}
	batch.accumulateBlockedCellBehavior(0, rewards)
	if err := batch.episodes[0].engine.ApplyVerifiedMovementCheckpoint(1, battleengine.PositionAtCellCenter(battleengine.Cell{Row: 1, Col: 1})); err != nil {
		t.Fatal(err)
	}
	beforeReentry := rewards[0]
	batch.accumulateBlockedCellBehavior(0, rewards)
	if got := rewards[0] - beforeReentry; math.Abs(float64(got+rewardRapidBlockedReentryPenalty)) > 1e-6 {
		t.Fatalf("rapid wall re-entry reward delta=%v, want %v", got, -rewardRapidBlockedReentryPenalty)
	}
}

func BenchmarkBatchObserveEightParticipants(b *testing.B) {
	config := testBatchConfig(8)
	config.ReuseTensorBuffers = true
	config.ParticipantCount = 8
	config.ParticipantCounts = []int{8}
	config.TeamCount = 4
	config.TeamLayouts = [][]uint8{{1, 1, 2, 2, 3, 3, 4, 4}}
	config.Maps[0] = benchmarkTrainingMap()
	batch, err := NewBatch(config)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := batch.Observe(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReusableTensorObservationClearsPreviousFrame(t *testing.T) {
	config := testBatchConfig(1)
	config.ReuseTensorBuffers = true
	batch, err := NewBatch(config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := batch.Observe()
	if err != nil {
		t.Fatal(err)
	}
	for index := range first.Spatial {
		first.Spatial[index] = 1
	}
	second, err := batch.Observe()
	if err != nil {
		t.Fatal(err)
	}
	for actorIndex := 0; actorIndex < second.ParticipantCount; actorIndex++ {
		base := actorIndex * SpatialChannels * second.Height * second.Width
		for cellIndex := 0; cellIndex < second.Height*second.Width; cellIndex++ {
			if second.Spatial[base+cellIndex] != 1 {
				t.Fatalf("frame base channel actor=%d cell=%d was not re-encoded", actorIndex, cellIndex)
			}
		}
	}
	// Channel 63 has no push-progress cells in this fixture. Any retained one
	// proves the reusable storage was not cleared before encoding.
	channelSize := second.Height * second.Width
	for actorIndex := 0; actorIndex < second.ParticipantCount; actorIndex++ {
		base := (actorIndex*SpatialChannels + 63) * channelSize
		for cellIndex := 0; cellIndex < channelSize; cellIndex++ {
			if second.Spatial[base+cellIndex] != 0 {
				t.Fatalf("stale spatial value actor=%d cell=%d", actorIndex, cellIndex)
			}
		}
	}
}

func testBatchConfig(envs int) Config {
	return Config{
		Maps: []mapdata.CompetitiveMap{testTrainingMap()}, EnvCount: envs, ParticipantCount: 2,
		TickMS: 20, DecisionMS: 100, TrapDurationMS: 6_000, DangerHorizonMS: 3_500, BaseSeed: 123,
	}
}

func testTrainingMap() mapdata.CompetitiveMap {
	field := mapdata.CompetitiveBattlefield{Width: 5, Height: 3, Cells: make([]mapdata.CompetitiveBattleCell, 15)}
	for index := range field.Cells {
		field.Cells[index] = mapdata.CompetitiveBattleCell{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true}
	}
	return mapdata.CompetitiveMap{
		ID: 1, Name: "training", Selectable: true, PlayerLimit: 2,
		NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary, RequiredItemField: 0,
		SpawnGroupA: []mapdata.CompetitiveCell{{Row: 1, Col: 0}},
		SpawnGroupB: []mapdata.CompetitiveCell{{Row: 1, Col: 4}},
		Battlefield: field,
	}
}

func benchmarkTrainingMap() mapdata.CompetitiveMap {
	const width, height = 15, 13
	field := mapdata.CompetitiveBattlefield{Width: width, Height: height, Cells: make([]mapdata.CompetitiveBattleCell, width*height)}
	for index := range field.Cells {
		field.Cells[index] = mapdata.CompetitiveBattleCell{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true}
	}
	spawnsA := []mapdata.CompetitiveCell{{Row: 1, Col: 1}, {Row: 1, Col: 13}, {Row: 11, Col: 1}, {Row: 11, Col: 13}}
	spawnsB := []mapdata.CompetitiveCell{{Row: 1, Col: 7}, {Row: 6, Col: 1}, {Row: 6, Col: 13}, {Row: 11, Col: 7}}
	return mapdata.CompetitiveMap{
		ID: 2, Name: "benchmark", Selectable: true, PlayerLimit: 8,
		NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary, RequiredItemField: 0,
		SpawnGroupA: spawnsA, SpawnGroupB: spawnsB, Battlefield: field,
	}
}

func TestRecordSelfEliminationsCountsOnlyOwnerTargetMatch(t *testing.T) {
	result := make([]uint8, 3)
	recordSelfEliminations(
		[]uint16{100, 200},
		[]battleengine.Event{
			{Kind: battleengine.EventActorEliminated, PlayerID: 100, TargetID: 100},
			{Kind: battleengine.EventActorEliminated, PlayerID: 100, TargetID: 200},
			{Kind: battleengine.EventActorTrapped, PlayerID: 200, TargetID: 200},
		},
		result,
	)
	if result[0] != 1 || result[1] != 0 || result[2] != 0 {
		t.Fatalf("self-elimination counts = %v, want [1 0 0]", result)
	}
}
