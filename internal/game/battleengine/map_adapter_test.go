package battleengine

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"qqtang/internal/game/mapdata"
)

func TestGridFromCompetitiveMapAcceptsOnlyOrdinaryRuleOne(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		ID: 1, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		Battlefield: mapdata.CompetitiveBattlefield{
			Width: 3, Height: 1,
			Cells: []mapdata.CompetitiveBattleCell{
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellBreakable, MapElementOccupied: true, Durability: 2, MapElementID: 10, PandaPushable: true, ElementWidth: 1, ElementHeight: 1, ElementAnchorCol: 1},
				{Collision: mapdata.CompetitiveCellSolid, FlamePassable: true, MapElementOccupied: true},
			},
		},
	}
	grid, err := GridFromCompetitiveMap(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := grid.Cells, []Tile{
		{Kind: CellOpen, FlamePassable: true},
		{Kind: CellBreakable, MapElementOccupied: true, Durability: 2, MapElementID: 10, PandaPushable: true, ElementWidth: 1, ElementHeight: 1, ElementAnchor: Cell{Col: 1}},
		{Kind: CellSolid, FlamePassable: true, MapElementOccupied: true},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("grid cells = %v, want %v", got, want)
	}
	entry.NativeRule = 2
	entry.Rule = mapdata.CompetitiveRuleKickBomb
	if _, err := GridFromCompetitiveMap(entry); err == nil {
		t.Fatal("special competitive rule was accepted")
	}
}

func TestShippedOrdinaryMapsProduceValidBattleGrids(t *testing.T) {
	root := filepath.Join("..", "..", "..", "runtime", "client-patched")
	if _, err := os.Stat(filepath.Join(root, "map", "mapDesc.py")); os.IsNotExist(err) {
		t.Skip("verified runtime client is not present")
	}
	catalog, err := mapdata.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	actorPassableMapElements := 0
	hiddenInvalid, spawnInvalid := 0, 0
	var firstHiddenInvalid, firstSpawnInvalid string
	var hiddenInvalidDetails []string
	for _, mapID := range catalog.AllCompetitiveIDs() {
		entry, ok := catalog.CompetitiveMap(mapID)
		if !ok || entry.NativeRule != 1 || entry.Rule != mapdata.CompetitiveRuleOrdinary {
			continue
		}
		grid, gridErr := GridFromCompetitiveMap(entry)
		if gridErr != nil {
			t.Fatalf("ordinary map %d: %v", mapID, gridErr)
		}
		if validateErr := grid.validate(); validateErr != nil {
			t.Fatalf("ordinary map %d grid: %v", mapID, validateErr)
		}
		for _, tile := range grid.Cells {
			if tile.Kind == CellOpen && tile.MapElementOccupied {
				actorPassableMapElements++
			}
		}
		for _, cell := range entry.HiddenItemCells {
			tile, present := grid.Cell(Cell{Row: int16(cell.Row), Col: int16(cell.Col)})
			if !present || tile.Kind != CellBreakable {
				hiddenInvalid++
				if firstHiddenInvalid == "" {
					firstHiddenInvalid = fmt.Sprintf("map %d cell %d,%d is %+v/%t", mapID, cell.Row, cell.Col, tile, present)
				}
				hiddenInvalidDetails = append(hiddenInvalidDetails, fmt.Sprintf("map %d cell %d,%d is %+v/%t", mapID, cell.Row, cell.Col, tile, present))
			}
		}
		spawnCount := len(entry.SpawnGroupA) + len(entry.SpawnGroupB)
		if spawnCount < int(entry.PlayerLimit) {
			t.Fatalf("ordinary map %d has %d native spawn cells for limit %d", mapID, spawnCount, entry.PlayerLimit)
		}
		if validateErr := validateSpawnPools(competitiveCells(entry.SpawnGroupA), competitiveCells(entry.SpawnGroupB)); validateErr != nil {
			t.Fatalf("ordinary map %d native spawn pools: %v", mapID, validateErr)
		}
		for _, cells := range [][]mapdata.CompetitiveCell{entry.SpawnGroupA, entry.SpawnGroupB} {
			for _, cell := range cells {
				tile, present := grid.Cell(Cell{Row: int16(cell.Row), Col: int16(cell.Col)})
				if !present || tile.Kind != CellOpen {
					spawnInvalid++
					if firstSpawnInvalid == "" {
						firstSpawnInvalid = fmt.Sprintf("map %d cell %d,%d is %+v/%t", mapID, cell.Row, cell.Col, tile, present)
					}
				}
			}
		}
		count++
	}
	if count == 0 {
		t.Fatal("shipped catalog contains no ordinary rule-1 maps")
	}
	if actorPassableMapElements == 0 {
		t.Fatal("shipped ordinary maps contain no player-passable static map-element cells")
	}
	if hiddenInvalid != 0 || spawnInvalid != 0 {
		t.Fatalf("native table/grid mismatches: hidden=%d (%s; all=%v), spawn=%d (%s)", hiddenInvalid, firstHiddenInvalid, hiddenInvalidDetails, spawnInvalid, firstSpawnInvalid)
	}
}

func TestConfigFromCompetitiveMapUsesRuleOneDuration(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		ID: 1, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		Battlefield: mapdata.CompetitiveBattlefield{
			Width: 2, Height: 1,
			Cells: []mapdata.CompetitiveBattleCell{
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
			},
		},
		SpawnGroupA: []mapdata.CompetitiveCell{{Row: 0, Col: 0}, {Row: 0, Col: 1}},
	}
	rules := testConfig().Rules
	rules.RoundDurationMS = 180_000
	rules.BombFuseMS = 123
	rules.FlameDurationMS = 456
	rules.ActorHalfSizePixels = 12
	rules.SpeedPixelsPerSecondByRate[3] = 999
	participants := []Participant{
		testParticipant(1, 1, ParticipantHuman, Cell{}),
		testParticipant(2, 2, ParticipantVirtualAI, Cell{Col: 1}),
	}
	for index := range participants {
		participants[index].SpeedRate = 3
		participants[index].MaxSpeedRate = 3
		participants[index].SpeedPixelsPerSecond = 0
	}
	config, err := ConfigFromCompetitiveMap(entry, CompetitiveMapConfigOptions{
		SimulationSeed: 7, SpawnSeed: 11, ItemSeed: 12, Rules: rules, Participants: participants,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Rules.RoundDurationMS != 240_000 {
		t.Fatalf("rule-1 duration = %d, want 240000", config.Rules.RoundDurationMS)
	}
	if StandardRoundTimeMS != 240_000 {
		t.Fatalf("standard duration constant = %d, want 240000", StandardRoundTimeMS)
	}
	if config.Rules.BombFuseMS != 3_000 {
		t.Fatalf("rule-1 bomb fuse = %d, want 3000", config.Rules.BombFuseMS)
	}
	if config.Rules.FlameDurationMS != 500 {
		t.Fatalf("rule-1 flame duration = %d, want 500", config.Rules.FlameDurationMS)
	}
	if config.Rules.ActorHalfSizePixels != NativeActorHalfSizePixels {
		t.Fatalf("rule-1 actor half-size = %d, want %d", config.Rules.ActorHalfSizePixels, NativeActorHalfSizePixels)
	}
	if config.Rules.SpeedPixelsPerSecondByRate[3] != 140 {
		t.Fatalf("rule-1 speed rate 3 = %d, want 140 pixels/second", config.Rules.SpeedPixelsPerSecondByRate[3])
	}
	config.Participants[0].PlayerID = 99
	if participants[0].PlayerID != 1 {
		t.Fatal("participants were not copied independently")
	}
}

func TestHiddenPickupsFromCompetitiveMapUsesEnhancedRolledWireItems(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		ID: 1, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		HiddenItemCells: []mapdata.CompetitiveCell{
			{Row: 1, Col: 1}, {Row: 1, Col: 2}, {Row: 1, Col: 3},
			{Row: 2, Col: 1}, {Row: 2, Col: 2}, {Row: 2, Col: 3},
		},
		WallItemRules: []mapdata.CompetitiveWallItemRule{
			{SceneID: SceneBombCapacitySmall, Minimum: 2, Maximum: 2, Probability: 1},
			{SceneID: SceneBombPowerSmall, Minimum: 1, Maximum: 1, Probability: 1},
		},
	}
	pickups, err := HiddenPickupsFromCompetitiveMap(entry, 0x13572468, 2)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[uint32]int{}
	for _, pickup := range pickups {
		counts[pickup.SceneID]++
		if pickup.State != PickupHidden {
			t.Fatalf("map pickup state = %d, want hidden", pickup.State)
		}
	}
	if !reflect.DeepEqual(counts, map[uint32]int{
		SceneBombCapacitySmall: 1,
		SceneBombCapacityLarge: 1,
		SceneBombPowerLarge:    1,
	}) {
		t.Fatalf("map pickup counts = %v", counts)
	}
}

func TestConfigFromCompetitiveMapUsesRecordedWallItemsWithoutReroll(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		ID: 1, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		Battlefield: mapdata.CompetitiveBattlefield{
			Width: 3, Height: 2,
			Cells: []mapdata.CompetitiveBattleCell{
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellBreakable, FlamePassable: true, Durability: 1},
				{Collision: mapdata.CompetitiveCellBreakable, FlamePassable: true, Durability: 1},
				{Collision: mapdata.CompetitiveCellBreakable, FlamePassable: true, Durability: 1},
			},
		},
		SpawnGroupA:     []mapdata.CompetitiveCell{{Row: 0, Col: 0}},
		SpawnGroupB:     []mapdata.CompetitiveCell{{Row: 0, Col: 2}},
		HiddenItemCells: []mapdata.CompetitiveCell{{Row: 1, Col: 0}, {Row: 1, Col: 1}, {Row: 1, Col: 2}},
		WallItemRules:   []mapdata.CompetitiveWallItemRule{{SceneID: SceneBombPowerSmall, Minimum: 3, Maximum: 3, Probability: 1}},
	}
	participants := []Participant{
		testParticipant(1, 1, ParticipantHuman, Cell{}),
		testParticipant(2, 2, ParticipantHuman, Cell{}),
	}
	config, err := ConfigFromCompetitiveMap(entry, CompetitiveMapConfigOptions{
		SpawnSeed: 1, ItemSeed: 2, SpawnMode: NativeSpawnFree, Rules: testConfig().Rules,
		Participants: participants, UseRecordedWallItems: true,
		RecordedWallItems: []mapdata.CompetitiveWallItem{{SceneID: SceneBombCapacitySmall, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Pickups) != 1 || config.Pickups[0].SceneID != SceneBombCapacitySmall {
		t.Fatalf("recorded pickups = %+v, want exact SceneID 1 list instead of local SceneID 2 roll", config.Pickups)
	}
}

func TestNativeRuntimeMapEligibilityRejectsAnyPossibleUnsupportedPickup(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		ID: 1, PlayerLimit: 2, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		Battlefield: mapdata.CompetitiveBattlefield{
			Width: 2, Height: 1,
			Cells: []mapdata.CompetitiveBattleCell{
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
				{Collision: mapdata.CompetitiveCellOpen, FlamePassable: true},
			},
		},
		WallItemRules: []mapdata.CompetitiveWallItemRule{
			{SceneID: SceneBombCapacitySmall, Minimum: 1, Maximum: 1, Probability: 1},
			{SceneID: 63, Minimum: 0, Maximum: 1, Probability: 0.01},
		},
	}
	if err := ValidateNativeRuntimeMap(entry); err == nil {
		t.Fatal("map eligibility depended on an unsupported pickup not rolling")
	}
	entry.WallItemRules[1].Probability = 0
	if err := ValidateNativeRuntimeMap(entry); err != nil {
		t.Fatalf("non-emitting unsupported rule rejected map: %v", err)
	}
}

func TestPublicWallItemProfileExposesDistributionWithoutHiddenLayout(t *testing.T) {
	entry := mapdata.CompetitiveMap{
		HiddenItemCells: make([]mapdata.CompetitiveCell, 10),
		WallItemRules: []mapdata.CompetitiveWallItemRule{
			{SceneID: SceneBombCapacitySmall, Minimum: 2, Maximum: 4, Probability: 0.5},
			{SceneID: SceneBombCapacityLarge, Minimum: 1, Maximum: 1, Probability: 0.25},
		},
	}
	profile := publicWallItemProfile(entry).Categories[WallItemBombCapacity]
	if got, want := profile.PresenceProbability, float32(0.625); got != want {
		t.Fatalf("capacity presence probability = %v, want %v", got, want)
	}
	if got, want := profile.ExpectedDensity, float32(0.175); math.Abs(float64(got-want)) > 1e-6 {
		t.Fatalf("capacity expected density = %v, want %v", got, want)
	}

	// The public summary must not depend on ItemSeed or on which hidden cells
	// were actually selected for this match.
	entry.HiddenItemCells[0] = mapdata.CompetitiveCell{Row: 99, Col: 99}
	changed := publicWallItemProfile(entry).Categories[WallItemBombCapacity]
	if changed != profile {
		t.Fatalf("public wall profile leaked hidden coordinates: got %+v, want %+v", changed, profile)
	}
}
