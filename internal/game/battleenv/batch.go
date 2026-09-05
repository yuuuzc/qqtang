package battleenv

import (
	"fmt"
	"runtime"
	"sort"
	"sync"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/mapdata"
)

var defaultRoleIDs = [...]uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15, 16, 18, 19, 20, 21, 22}

type Config struct {
	Maps     []mapdata.CompetitiveMap
	EnvCount int
	// ParticipantCount is the fixed tensor capacity. ParticipantCounts may
	// select a smaller real actor count on each reset; unused tensor slots stay
	// inactive so one policy update can mix 2/4/8-player episodes without
	// changing model or checkpoint shapes.
	ParticipantCount  int
	ParticipantCounts []int
	TeamCount         int
	// TeamLayouts optionally supplies one or more explicit TeamID layouts. Each
	// layout has ParticipantCount entries and at least two teams. A deterministic
	// layout is selected on every reset. When empty, TeamCount preserves the
	// legacy equal-sized layout.
	TeamLayouts [][]uint8
	// TeamSpawnPermille mixes the client's free-room and equal two-team spawn
	// consumers when the chosen topology supports both. Uneven/multi-team
	// layouts always use the native free-room consumer.
	TeamSpawnPermille uint16
	// DuelCurriculumPermille replaces a sampled two-actor opening with a legal
	// late-game state: breakable terrain is already cleared, opponents start a
	// few cells apart and native attributes are partially developed. Combat and
	// outcome rules remain unchanged; this only makes rare endgame blockade
	// states frequent enough for policy-gradient training.
	DuelCurriculumPermille uint16
	// DuelCurriculumBombCapacity optionally fixes only the cleared combat
	// course capacity. Zero retains the role-derived developed value.
	DuelCurriculumBombCapacity byte
	// BlockadeCurriculumPermille samples a cleared legal duel whose two spawns
	// are both in nearby low-degree corridors. Outcomes remain ordinary combat
	// wins; the topology only makes rare escape-denial evidence frequent.
	BlockadeCurriculumPermille uint16
	// ChainFinisherCurriculumPermille samples a cleared 1v1 conversion state
	// with one already-visible owned bubble approaching detonation. The attacker
	// and defender keep ordinary engine actions and outcomes; the reset merely
	// makes the expert move-to-blast-line -> late place-and-depart opportunity
	// common enough for self-play to discover and counter.
	ChainFinisherCurriculumPermille uint16
	// BombEscapeCurriculumPermille samples a short, training-only capability
	// episode in the same legal cleared duel state. The episode succeeds only
	// after an actor's own bomb explodes while that actor remains active. This
	// turns the complete place-bomb -> leave-blast sequence into a dense outcome
	// without masking actions or changing any live battle rule.
	BombEscapeCurriculumPermille uint16
	// Successive capability stages may expose more native bubble capacity and
	// require multiple completed safe detonations. Zero keeps the conservative
	// single-capacity/single-cycle defaults for existing callers.
	BombEscapeCapacity            byte
	BombEscapeRequiredDetonations byte
	// WorkerCount controls independent environment execution. Zero uses the
	// current Go scheduler parallelism; one preserves serial execution.
	WorkerCount     int
	TickMS          uint32
	DecisionMS      uint32
	TrapDurationMS  uint32
	DangerHorizonMS uint32
	BaseSeed        uint64
	// ReuseTensorBuffers lets a streaming caller reuse the large observation
	// backing arrays between requests. A returned TensorBatch then remains valid
	// only until the next Observe or Step call on this Batch. The binary training
	// worker writes each response before reading another request, so it can use
	// this mode without retaining stale views. Ordinary Go callers keep the
	// default independent-return semantics.
	ReuseTensorBuffers bool
}

type AgentMetrics struct {
	MovedTicks       uint32
	BombsPlaced      uint32
	WallsDestroyed   uint32
	Pickups          uint32
	Traps            uint32
	TrapAssists      uint32
	Eliminations     uint32
	SelfEliminations uint32
	Rescues          uint32
	ActionsUsed      uint32
	NativePasses     uint32
	BlockedEntries   uint32
	BlockedTicks     uint32
	LateStallTicks   uint32
}

type EpisodeMetrics struct {
	MapID                   uint32
	DuelCurriculum          bool
	BlockadeCurriculum      bool
	ChainFinisherCurriculum bool
	BombEscapeCurriculum    bool
	Decisions               uint32
	Ticks                   uint32
	TimedOut                bool
	Agents                  []AgentMetrics
}

const (
	// Placing a bomb is an action, not progress by itself. Rewarding every
	// placement lets a policy farm return by spamming harmless bombs; useful
	// placements already receive delayed credit through wall, debuff, trap,
	// elimination, pickup and terminal events.
	rewardBombPlaced    float32 = 0
	rewardWallDestroyed float32 = 0.02
	rewardPickup        float32 = 0.01
	// Opening development bonuses are finite because walls and pickups are
	// consumed.  They make a safe resource route competitive with speculative
	// early pursuit without creating a repeatable reward loop.
	rewardDevelopmentWallBonus   float32 = 0.01
	rewardDevelopmentPickupBonus float32 = 0.03
	rewardMovementDebuff         float32 = 0.05
	// A terminal win can also happen because an opponent traps itself.  Give
	// substantially more causal credit to a bubble that actually traps an
	// enemy, then to the resulting elimination, so an attacking policy is more
	// valuable than walking safely until the other team makes a mistake.
	rewardTrap float32 = 0.6
	// A second actor whose bubble/flame closes an immediate escape cell shares
	// causal credit for a resulting trap.  This is one bounded pool across all
	// assistants, not a per-bomb bonus, so surrounding a victim with more bombs
	// cannot multiply reward without bound.
	rewardTrapAssistTotal float32 = 0.6
	rewardElimination     float32 = 1.0
	rewardRescue          float32 = 0.2
	// Repeated legitimate rescues decay geometrically against this per-target
	// episode budget: 0.20, 0.10, 0.05... and can never exceed 0.40 total.
	rewardRescueBudget float32 = rewardRescue * 2
	// Breaking a visible transformation consumes an opponent's temporary extra
	// life, but remains less valuable than trapping or eliminating the actor.
	rewardTransformationBroken float32 = 0.1
	// The opening is a development phase.  Do not pay the learner merely for
	// closing distance before it has had time to open terrain and collect the
	// visible resource upgrades that make a real midgame attack sustainable.
	// Opportunistic traps and eliminations still keep their ordinary causal
	// rewards; only the generic chase potential is delayed.
	rewardDevelopmentEndMS uint32 = 30_000
	// Potential shaping guides an actor toward the nearest currently observable
	// enemy without rewarding loops: approaching earns exactly what moving away
	// later gives back. Terrain destruction remains the complementary signal
	// when a direct route is blocked.
	rewardEnemyApproachPerCell float32 = 0.005
	// Decisive play is preferable to surviving until the clock. A fast win may
	// double its terminal reward, while an immediate throw is punished more
	// heavily than a late loss. Timeout draws are evaluated separately from the
	// public initial/live team material: an outnumbered survivor must never learn
	// that deliberate elimination is preferable to holding the round.
	rewardEarlyWinScale  float32 = 1.0
	rewardEarlyLossScale float32 = 1.0
	// Outcomes are normalized against the public initial team-size prior. Equal
	// teams retain the familiar +1/0/-1 late win/draw/loss values, while a 1v7
	// win is worth +1.75 and a routine 7v1 win +0.25. In K-team rounds the
	// K/(K-1) scale preserves zero-sum rewards and a +1 equal-team win. A timeout
	// distributes the unit result by each team's surviving fraction, so it stays
	// strictly below a decisive win while rewarding difficult survival.
	rewardDraw float32 = 0
	// From 90 seconds of controllable play onward, a team that has made no
	// material progress for 15 seconds pays a continuous cost. This is earlier
	// than the mathematical half of the 237-second native round because normal
	// rule-one matches are expected to resolve around the one-to-two minute
	// window; waiting until 118.5 seconds left a measurable timeout tail in the
	// current-engine promotion gate. Movement and bomb
	// spam do not reset it; destroying terrain, collecting an item, affecting an
	// enemy, rescuing a teammate, or eliminating an enemy does.  The terminal
	// outcome remains much larger than this shaping signal.
	rewardRoundDurationMS           uint32  = battleengine.StandardRoundTimeMS - battleengine.NativeRoundStartClockMS
	rewardLateStallStartMS          uint32  = 90_000
	rewardLateStallProgressGraceMS  uint32  = 15_000
	rewardLateStallPenaltyPerSecond float32 = 0.01
	rewardLateStallMaxMultiplier    float32 = 3.0
	// Native bubble traversal is not protected and remains unrestricted. Only
	// entering a static blocked cell (the wall-invulnerability technique) is
	// shaped: a few tactical entries are free, immediate re-entry and prolonged
	// wall occupancy are progressively unprofitable.
	rewardFreeBlockedEntries          uint32  = 2
	rewardRepeatedBlockedEntryPenalty float32 = 0.02
	rewardBlockedEntryPenaltyCap      float32 = 0.1
	rewardBlockedReentryWindowMS      uint32  = 5_000
	rewardRapidBlockedReentryPenalty  float32 = 0.05
	rewardBlockedCellGraceMS          uint32  = 1_200
	rewardBlockedCellPenaltyPerSecond float32 = 0.025
	// Friendly fire remains a useful negative signal, but the terminal result
	// is the objective.  Keep the attacker's shaping penalty small enough that
	// a tactically useful blast (or a later rescue) is not dominated by it.
	rewardFriendlyFireFactor float32 = 0.1
)

type teamMaterial struct {
	initial int
	live    int
}

func materialByTeam(actors []battleengine.Actor) map[byte]teamMaterial {
	result := make(map[byte]teamMaterial)
	for _, actor := range actors {
		material := result[actor.TeamID]
		material.initial++
		if actor.State != battleengine.ActorEliminated {
			material.live++
		}
		result[actor.TeamID] = material
	}
	return result
}

func timeoutDrawReward(teamID byte, material map[byte]teamMaterial) float32 {
	team := material[teamID]
	if team.initial <= 0 || len(material) < 2 {
		return rewardDraw
	}
	totalRetention := float32(0)
	for _, candidate := range material {
		if candidate.initial > 0 {
			totalRetention += float32(candidate.live) / float32(candidate.initial)
		}
	}
	if totalRetention <= 0 {
		return initialOutcomeReward(teamID, 1/float32(len(material)), material)
	}
	teamRetention := float32(team.live) / float32(team.initial)
	return initialOutcomeReward(teamID, teamRetention/totalRetention, material)
}

func initialOutcomeReward(teamID byte, score float32, material map[byte]teamMaterial) float32 {
	team := material[teamID]
	// A coalition's combat strength is not linear in its seat count: members
	// add both individual material and pairwise opportunities to cooperate.
	// Squared team size is the cold-start prior until an exact-layout mirror
	// baseline is available to the promotion evaluator.  It distinguishes one
	// actor facing a seven-person coalition from eight mutually hostile solo
	// teams, even though both contain eight actors.
	totalInitialStrength := 0
	for _, candidate := range material {
		totalInitialStrength += candidate.initial * candidate.initial
	}
	teamCount := len(material)
	if team.initial <= 0 || totalInitialStrength <= 0 || teamCount < 2 {
		return 2*score - 1
	}
	expected := float32(team.initial*team.initial) / float32(totalInitialStrength)
	scale := float32(teamCount) / float32(teamCount-1)
	return scale * (score - expected)
}

func terminalOutcomeReward(teamID byte, outcome battleengine.Outcome, material map[byte]teamMaterial) float32 {
	if !outcome.Ended {
		return 0
	}
	if outcome.Draw {
		if outcome.TimedOut {
			return timeoutDrawReward(teamID, material)
		}
		return initialOutcomeReward(teamID, 1/float32(len(material)), material)
	}
	elapsed := outcome.EndedAtMS
	if elapsed > rewardRoundDurationMS {
		elapsed = rewardRoundDurationMS
	}
	remaining := float32(rewardRoundDurationMS-elapsed) / float32(rewardRoundDurationMS)
	if teamID == outcome.WinnerTeamID {
		base := initialOutcomeReward(teamID, 1, material)
		return base * (1 + rewardEarlyWinScale*remaining)
	}
	base := initialOutcomeReward(teamID, 0, material)
	return base * (1 + rewardEarlyLossScale*remaining)
}

type rewardDelta struct {
	playerID uint16
	amount   float32
}

// trapRewardCredit is deliberately reversible.  A trap is a near-terminal
// advantage only while it remains unresolved; a native teammate rescue must
// remove the attacker's/direct-assistant credit and the victim's penalty.
type trapRewardCredit struct {
	deltas      []rewardDelta
	enemyCaused bool
}

type StepResult struct {
	Observation      TensorBatch
	Rewards          []float32
	Dones            []uint8
	SelfEliminations []uint8
	Outcomes         []battleengine.Outcome
	Events           [][]battleengine.Event
}

type episode struct {
	engine       *battleengine.Engine
	playerIDs    []uint16
	teamIDs      []uint8
	episodeIndex uint64
	metrics      EpisodeMetrics
	training     []agentTrainingState
	teamProgress map[uint8]uint32
	trapCredits  map[uint16]trapRewardCredit
	// Rescue shaping spends a bounded per-target episode budget. Repeated
	// trap/rescue cycles therefore decay toward zero instead of minting an
	// unbounded positive return.
	rescueRewardPaid map[uint16]float32
}

type agentTrainingState struct {
	blocked           bool
	hasBlockedExit    bool
	lastBlockedExitMS uint32
	blockedTicks      uint32
	hasEnemyDistance  bool
	enemyDistance     int
	safeDetonations   byte
}

type Batch struct {
	config    Config
	maps      []mapdata.CompetitiveMap
	episodes  []episode
	maxWidth  int
	maxHeight int
	workers   int
	tensors   TensorBatch
}

type fixedSearchCandidates []battleengine.ScoredAction

func (candidates fixedSearchCandidates) CandidateActions(_ battleengine.Observation, _ []battleengine.Action, limit int) ([]battleengine.ScoredAction, error) {
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	return append([]battleengine.ScoredAction(nil), candidates[:limit]...), nil
}

// LoadEligibleMaps returns only selectable no-item ordinary maps whose entire
// native wall-item table is supported by the restricted engine.
func LoadEligibleMaps(clientRoot string, participantCount int) ([]mapdata.CompetitiveMap, error) {
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		return nil, err
	}
	var result []mapdata.CompetitiveMap
	for _, mapID := range catalog.CompetitiveIDs() {
		entry, ok := catalog.CompetitiveMap(mapID)
		if !ok || entry.RequiredItemField != 0 || entry.NativeRule != 1 || !entry.UsesOrdinaryElimination() || int(entry.PlayerLimit) < participantCount {
			continue
		}
		if validateErr := battleengine.ValidateNativeRuntimeMap(entry); validateErr != nil {
			continue
		}
		freeTeams := make([]byte, participantCount)
		for index := range freeTeams {
			freeTeams[index] = byte(index + 1)
		}
		if validateErr := battleengine.ValidateNativeSpawnTopology(entry, battleengine.NativeSpawnFree, freeTeams); validateErr != nil {
			continue
		}
		result = append(result, entry)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no eligible no-item ordinary maps for %d participants", participantCount)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func NewBatch(config Config) (*Batch, error) {
	if config.EnvCount <= 0 {
		return nil, fmt.Errorf("environment count must be positive")
	}
	if config.ParticipantCount < 2 || config.ParticipantCount > battleengine.MaxParticipants {
		return nil, fmt.Errorf("participant count %d is outside 2..%d", config.ParticipantCount, battleengine.MaxParticipants)
	}
	if len(config.ParticipantCounts) == 0 {
		config.ParticipantCounts = []int{config.ParticipantCount}
	}
	allowedParticipantCounts := make(map[int]struct{}, len(config.ParticipantCounts))
	for _, participantCount := range config.ParticipantCounts {
		if participantCount < 2 || participantCount > config.ParticipantCount {
			return nil, fmt.Errorf("sampled participant count %d is outside 2..%d", participantCount, config.ParticipantCount)
		}
		allowedParticipantCounts[participantCount] = struct{}{}
	}
	if len(config.TeamLayouts) == 0 {
		if config.TeamCount == 0 {
			config.TeamCount = 2
		}
		for _, participantCount := range config.ParticipantCounts {
			if config.TeamCount < 2 || config.TeamCount > participantCount || participantCount%config.TeamCount != 0 {
				return nil, fmt.Errorf("team count %d must divide %d participants and stay within 2..%d", config.TeamCount, participantCount, participantCount)
			}
			teamSize := participantCount / config.TeamCount
			layout := make([]uint8, participantCount)
			for index := range layout {
				layout[index] = uint8(index/teamSize + 1)
			}
			config.TeamLayouts = append(config.TeamLayouts, layout)
		}
	}
	if config.TeamSpawnPermille > 1000 {
		return nil, fmt.Errorf("team spawn probability %d is outside 0..1000", config.TeamSpawnPermille)
	}
	if config.DuelCurriculumPermille > 1000 {
		return nil, fmt.Errorf("duel curriculum probability %d is outside 0..1000", config.DuelCurriculumPermille)
	}
	if config.BombEscapeCurriculumPermille > 1000 {
		return nil, fmt.Errorf("bomb escape curriculum probability %d is outside 0..1000", config.BombEscapeCurriculumPermille)
	}
	if config.BlockadeCurriculumPermille > 1000 {
		return nil, fmt.Errorf("blockade curriculum probability %d is outside 0..1000", config.BlockadeCurriculumPermille)
	}
	if config.ChainFinisherCurriculumPermille > 1000 {
		return nil, fmt.Errorf("chain finisher curriculum probability %d is outside 0..1000", config.ChainFinisherCurriculumPermille)
	}
	curriculumPermille := uint32(config.DuelCurriculumPermille) +
		uint32(config.BlockadeCurriculumPermille) + uint32(config.ChainFinisherCurriculumPermille) +
		uint32(config.BombEscapeCurriculumPermille)
	if curriculumPermille > 1000 {
		return nil, fmt.Errorf(
			"combined duel curriculum probability %d is outside 0..1000", curriculumPermille,
		)
	}
	if config.BombEscapeCapacity == 0 {
		config.BombEscapeCapacity = 1
	}
	if config.BombEscapeRequiredDetonations == 0 {
		config.BombEscapeRequiredDetonations = 1
	}
	if config.WorkerCount < 0 {
		return nil, fmt.Errorf("worker count %d cannot be negative", config.WorkerCount)
	}
	layoutCounts := make(map[int]int, len(allowedParticipantCounts))
	for index, layout := range config.TeamLayouts {
		if _, allowed := allowedParticipantCounts[len(layout)]; !allowed {
			return nil, fmt.Errorf("team layout %d has unsupported participant count %d", index, len(layout))
		}
		if err := validateTeamLayout(layout, len(layout)); err != nil {
			return nil, fmt.Errorf("team layout %d: %w", index, err)
		}
		config.TeamLayouts[index] = append([]uint8(nil), layout...)
		layoutCounts[len(layout)]++
	}
	for participantCount := range allowedParticipantCounts {
		if layoutCounts[participantCount] == 0 {
			return nil, fmt.Errorf("sampled participant count %d has no team layout", participantCount)
		}
	}
	if config.TickMS == 0 || config.DecisionMS == 0 || config.DecisionMS%config.TickMS != 0 {
		return nil, fmt.Errorf("decision interval %d must be a positive multiple of tick %d", config.DecisionMS, config.TickMS)
	}
	if config.TrapDurationMS == 0 {
		return nil, fmt.Errorf("offline training requires an explicit trap duration")
	}
	if config.DangerHorizonMS == 0 {
		config.DangerHorizonMS = battleengine.NativeBombFuseMS
	}
	if len(config.Maps) == 0 {
		return nil, fmt.Errorf("training map pool is empty")
	}
	workers := config.WorkerCount
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > config.EnvCount {
		workers = config.EnvCount
	}
	batch := &Batch{config: config, maps: append([]mapdata.CompetitiveMap(nil), config.Maps...), episodes: make([]episode, config.EnvCount), workers: workers}
	for _, entry := range batch.maps {
		if entry.RequiredItemField != 0 || entry.NativeRule != 1 || !entry.UsesOrdinaryElimination() || int(entry.PlayerLimit) < config.ParticipantCount {
			return nil, fmt.Errorf("map %d is outside restricted no-item rule-1 scope", entry.ID)
		}
		if err := battleengine.ValidateNativeRuntimeMap(entry); err != nil {
			return nil, err
		}
		if int(entry.Battlefield.Width) > batch.maxWidth {
			batch.maxWidth = int(entry.Battlefield.Width)
		}
		if int(entry.Battlefield.Height) > batch.maxHeight {
			batch.maxHeight = int(entry.Battlefield.Height)
		}
	}
	if err := batch.ResetAll(); err != nil {
		return nil, err
	}
	return batch, nil
}

func (batch *Batch) ResetAll() error {
	indices := make([]int, len(batch.episodes))
	for index := range indices {
		indices[index] = index
	}
	return batch.Reset(indices)
}

func (batch *Batch) Reset(indices []int) error {
	if batch == nil {
		return fmt.Errorf("training batch is nil")
	}
	seen := make(map[int]struct{}, len(indices))
	for _, envIndex := range indices {
		if envIndex < 0 || envIndex >= len(batch.episodes) {
			return fmt.Errorf("environment index %d is outside batch", envIndex)
		}
		if _, duplicate := seen[envIndex]; duplicate {
			return fmt.Errorf("environment index %d is repeated", envIndex)
		}
		seen[envIndex] = struct{}{}
	}
	return batch.parallelIndices(indices, func(envIndex int) error {
		if err := batch.resetEpisode(envIndex); err != nil {
			return fmt.Errorf("reset environment %d: %w", envIndex, err)
		}
		return nil
	})
}

func (batch *Batch) resetEpisode(envIndex int) error {
	episode := &batch.episodes[envIndex]
	seed := splitMix64(batch.config.BaseSeed + uint64(envIndex)*0x9e3779b97f4a7c15 + episode.episodeIndex*0xbf58476d1ce4e5b9)
	spawnSeed := uint32(splitMix64(seed))
	itemSeed := uint32(splitMix64(seed ^ 0x94d049bb133111eb))
	layoutSeed := splitMix64(seed ^ 0x2f6e2b1d5a8c39e7)
	participantCount := batch.config.ParticipantCounts[int(layoutSeed%uint64(len(batch.config.ParticipantCounts)))]
	matchingLayouts := make([][]uint8, 0, len(batch.config.TeamLayouts))
	for _, layout := range batch.config.TeamLayouts {
		if len(layout) == participantCount {
			matchingLayouts = append(matchingLayouts, layout)
		}
	}
	teamIDs := append([]uint8(nil), matchingLayouts[int(splitMix64(layoutSeed)%uint64(len(matchingLayouts)))]...)
	participants := make([]battleengine.Participant, participantCount)
	playerIDs := make([]uint16, participantCount)
	for index := range participants {
		playerID := uint16(index + 1)
		teamID := teamIDs[index]
		roleID := defaultRoleIDs[int(splitMix64(seed+uint64(index)+1)%uint64(len(defaultRoleIDs)))]
		participant, err := battleengine.ParticipantFromNativeRole(playerID, roleID, teamID, battleengine.ParticipantVirtualAI)
		if err != nil {
			return err
		}
		participants[index] = participant
		playerIDs[index] = playerID
	}
	spawnMode := battleengine.NativeSpawnFree
	spawnRoll := splitMix64(seed^0x8f4d2b71935ac6e1) % 1000
	if balancedNativeTeamLayout(teamIDs) && spawnRoll < uint64(batch.config.TeamSpawnPermille) {
		spawnMode = battleengine.NativeSpawnTeams
	}
	entry, err := batch.selectEpisodeMap(seed, spawnMode, teamIDs)
	if err != nil {
		return err
	}
	config, err := battleengine.ConfigFromCompetitiveMap(entry, battleengine.CompetitiveMapConfigOptions{
		SimulationSeed: seed, SpawnSeed: spawnSeed, ItemSeed: itemSeed, SpawnMode: spawnMode,
		Rules:        battleengine.Rules{TickMS: batch.config.TickMS, TrapDurationMS: batch.config.TrapDurationMS},
		Participants: participants,
	})
	if err != nil {
		return err
	}
	curriculumRoll := splitMix64(seed^0x6a09e667f3bcc909) % 1000
	bombEscapeCurriculum := participantCount == 2 &&
		curriculumRoll < uint64(batch.config.BombEscapeCurriculumPermille)
	blockadeThreshold := uint64(batch.config.BombEscapeCurriculumPermille) + uint64(batch.config.BlockadeCurriculumPermille)
	blockadeCurriculum := participantCount == 2 && !bombEscapeCurriculum && curriculumRoll < blockadeThreshold
	chainFinisherThreshold := blockadeThreshold + uint64(batch.config.ChainFinisherCurriculumPermille)
	chainFinisherCurriculum := participantCount == 2 && !bombEscapeCurriculum && !blockadeCurriculum &&
		curriculumRoll < chainFinisherThreshold
	duelCurriculum := participantCount == 2 && !bombEscapeCurriculum && !blockadeCurriculum && !chainFinisherCurriculum &&
		curriculumRoll < chainFinisherThreshold+uint64(batch.config.DuelCurriculumPermille)
	var chainSetup chainFinisherSetup
	if bombEscapeCurriculum {
		if err := applyBombEscapeCurriculum(&config, seed, batch.config.BombEscapeCapacity); err != nil {
			return fmt.Errorf("apply bomb escape curriculum: %w", err)
		}
	} else if blockadeCurriculum {
		if err := applyBlockadeCurriculum(&config, seed); err != nil {
			return fmt.Errorf("apply blockade curriculum: %w", err)
		}
		if batch.config.DuelCurriculumBombCapacity != 0 {
			if err := setCurriculumBombCapacity(&config, batch.config.DuelCurriculumBombCapacity); err != nil {
				return fmt.Errorf("apply blockade curriculum capacity: %w", err)
			}
		}
	} else if chainFinisherCurriculum {
		chainSetup, err = applyChainFinisherCurriculum(&config, seed)
		if err != nil {
			return fmt.Errorf("apply chain finisher curriculum: %w", err)
		}
	} else if duelCurriculum {
		if err := applyDuelCurriculum(&config, seed); err != nil {
			return fmt.Errorf("apply duel curriculum: %w", err)
		}
		if batch.config.DuelCurriculumBombCapacity != 0 {
			if err := setCurriculumBombCapacity(&config, batch.config.DuelCurriculumBombCapacity); err != nil {
				return fmt.Errorf("apply duel curriculum capacity: %w", err)
			}
		}
	}
	engine, err := battleengine.New(config)
	if err != nil {
		return err
	}
	if chainFinisherCurriculum {
		placedAtMS := engine.ElapsedMS() - (config.Rules.BombFuseMS - chainSetup.remainingMS)
		if _, err := engine.ApplyVerifiedBombPlacementAt(
			chainSetup.ownerID, chainSetup.root, uint16(chainSetup.power), 0, placedAtMS,
		); err != nil {
			return fmt.Errorf("seed chain finisher root bubble: %w", err)
		}
	}
	episode.engine = engine
	episode.playerIDs = playerIDs
	episode.teamIDs = teamIDs
	episode.episodeIndex++
	episode.metrics = EpisodeMetrics{
		MapID: entry.ID, DuelCurriculum: duelCurriculum,
		BlockadeCurriculum:      blockadeCurriculum,
		ChainFinisherCurriculum: chainFinisherCurriculum,
		BombEscapeCurriculum:    bombEscapeCurriculum,
		Agents:                  make([]AgentMetrics, len(playerIDs)),
	}
	episode.training = make([]agentTrainingState, len(playerIDs))
	episode.teamProgress = make(map[uint8]uint32, len(teamIDs))
	for _, teamID := range teamIDs {
		episode.teamProgress[teamID] = 0
	}
	episode.trapCredits = make(map[uint16]trapRewardCredit, len(playerIDs))
	episode.rescueRewardPaid = make(map[uint16]float32, len(playerIDs))
	return nil
}

type duelSpawnPair struct {
	first  battleengine.Cell
	second battleengine.Cell
}

func applyDuelCurriculum(config *battleengine.Config, seed uint64) error {
	if config == nil || len(config.Participants) != 2 {
		return fmt.Errorf("duel curriculum requires exactly two participants")
	}
	config.Grid = config.Grid.Clone()
	for index, tile := range config.Grid.Cells {
		if tile.Kind == battleengine.CellBreakable {
			config.Grid.Cells[index] = battleengine.Tile{Kind: battleengine.CellOpen, FlamePassable: true}
		}
	}
	config.Pickups = nil
	config.PublicWallItemProfile = battleengine.PublicWallItemProfile{}

	component := make([]int, len(config.Grid.Cells))
	for index := range component {
		component[index] = -1
	}
	var open []battleengine.Cell
	componentID := 0
	cardinal := [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	for index, tile := range config.Grid.Cells {
		if tile.Kind != battleengine.CellOpen || tile.MapElementOccupied || component[index] >= 0 {
			continue
		}
		start := battleengine.Cell{Row: int16(index / int(config.Grid.Width)), Col: int16(index % int(config.Grid.Width))}
		queue := []battleengine.Cell{start}
		component[index] = componentID
		for len(queue) != 0 {
			cell := queue[0]
			queue = queue[1:]
			open = append(open, cell)
			for _, delta := range cardinal {
				next := battleengine.Cell{Row: cell.Row + delta.Row, Col: cell.Col + delta.Col}
				tile, inside := config.Grid.Cell(next)
				if !inside || tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
					continue
				}
				nextIndex := int(next.Row)*int(config.Grid.Width) + int(next.Col)
				if component[nextIndex] >= 0 {
					continue
				}
				component[nextIndex] = componentID
				queue = append(queue, next)
			}
		}
		componentID++
	}
	var preferred, fallback []duelSpawnPair
	for left := 0; left < len(open); left++ {
		leftIndex := int(open[left].Row)*int(config.Grid.Width) + int(open[left].Col)
		for right := left + 1; right < len(open); right++ {
			rightIndex := int(open[right].Row)*int(config.Grid.Width) + int(open[right].Col)
			if component[leftIndex] != component[rightIndex] {
				continue
			}
			distance := absInt(int(open[left].Row-open[right].Row)) + absInt(int(open[left].Col-open[right].Col))
			if distance < 2 {
				continue
			}
			pair := duelSpawnPair{first: open[left], second: open[right]}
			fallback = append(fallback, pair)
			if distance >= 3 && distance <= 6 {
				preferred = append(preferred, pair)
			}
		}
	}
	pairs := preferred
	if len(pairs) == 0 {
		pairs = fallback
	}
	if len(pairs) == 0 {
		return fmt.Errorf("cleared map has no connected duel spawn pair")
	}
	pair := pairs[int(splitMix64(seed^0xbb67ae8584caa73b)%uint64(len(pairs)))]
	if splitMix64(seed^0x3c6ef372fe94f82b)&1 != 0 {
		pair.first, pair.second = pair.second, pair.first
	}
	config.Participants[0].Spawn = pair.first
	config.Participants[1].Spawn = pair.second
	for index := range config.Participants {
		participant := &config.Participants[index]
		attributeSeed := seed + uint64(index+1)*0x9e3779b97f4a7c15
		participant.BombCapacity = developedDuelAttribute(participant.BombCapacity, participant.MaxBombCapacity, attributeSeed, 2)
		participant.BombPower = developedDuelAttribute(participant.BombPower, participant.MaxBombPower, attributeSeed^0xa54ff53a5f1d36f1, 2)
		participant.SpeedRate = developedDuelAttribute(participant.SpeedRate, participant.MaxSpeedRate, attributeSeed^0x510e527fade682d1, participant.SpeedRate)
	}
	return nil
}

func setCurriculumBombCapacity(config *battleengine.Config, capacity byte) error {
	if config == nil || capacity == 0 {
		return fmt.Errorf("curriculum bomb capacity must be positive")
	}
	for index := range config.Participants {
		participant := &config.Participants[index]
		if capacity > participant.MaxBombCapacity {
			return fmt.Errorf(
				"capacity %d exceeds player %d native maximum %d",
				capacity, participant.PlayerID, participant.MaxBombCapacity,
			)
		}
		participant.BombCapacity = capacity
	}
	return nil
}

func openNeighborCount(grid battleengine.Grid, cell battleengine.Cell) int {
	directions := [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	count := 0
	for _, direction := range directions {
		next := battleengine.Cell{Row: cell.Row + direction.Row, Col: cell.Col + direction.Col}
		tile, inside := grid.Cell(next)
		if inside && tile.Kind == battleengine.CellOpen && !tile.MapElementOccupied {
			count++
		}
	}
	return count
}

func openPathDistance(grid battleengine.Grid, start, target battleengine.Cell, maximum int) (int, bool) {
	if start == target {
		return 0, true
	}
	type pathNode struct {
		cell     battleengine.Cell
		distance int
	}
	directions := [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	queue := []pathNode{{cell: start}}
	visited := map[battleengine.Cell]struct{}{start: {}}
	for len(queue) != 0 {
		node := queue[0]
		queue = queue[1:]
		if node.distance >= maximum {
			continue
		}
		for _, direction := range directions {
			next := battleengine.Cell{Row: node.cell.Row + direction.Row, Col: node.cell.Col + direction.Col}
			if _, seen := visited[next]; seen {
				continue
			}
			tile, inside := grid.Cell(next)
			if !inside || tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
				continue
			}
			distance := node.distance + 1
			if next == target {
				return distance, true
			}
			visited[next] = struct{}{}
			queue = append(queue, pathNode{cell: next, distance: distance})
		}
	}
	return 0, false
}

func applyBlockadeCurriculum(config *battleengine.Config, seed uint64) error {
	if err := applyDuelCurriculum(config, seed); err != nil {
		return err
	}
	type blockadeCell struct {
		cell  battleengine.Cell
		exits int
	}
	candidatesByPlayer := [2][]blockadeCell{}
	for player := range config.Participants {
		power := config.Participants[player].BombPower
		for index, tile := range config.Grid.Cells {
			if tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
				continue
			}
			cell := battleengine.Cell{Row: int16(index / int(config.Grid.Width)), Col: int16(index % int(config.Grid.Width))}
			exits := openNeighborCount(config.Grid, cell)
			if exits == 0 || exits > 2 || !bombEscapeRouteExists(config.Grid, cell, power, bombEscapeMaximumPathCells) {
				continue
			}
			candidatesByPlayer[player] = append(candidatesByPlayer[player], blockadeCell{cell: cell, exits: exits})
		}
	}
	var preferred, fallback []duelSpawnPair
	for _, first := range candidatesByPlayer[0] {
		for _, second := range candidatesByPlayer[1] {
			distance, connected := openPathDistance(config.Grid, first.cell, second.cell, 6)
			if !connected || distance < 2 {
				continue
			}
			pair := duelSpawnPair{first: first.cell, second: second.cell}
			fallback = append(fallback, pair)
			if distance <= 4 && (first.exits == 1 || second.exits == 1) {
				preferred = append(preferred, pair)
			}
		}
	}
	pairs := preferred
	if len(pairs) == 0 {
		pairs = fallback
	}
	if len(pairs) == 0 {
		return fmt.Errorf("cleared map has no connected escapable low-degree blockade spawn pair")
	}
	pair := pairs[int(splitMix64(seed^0x5be0cd19137e2179)%uint64(len(pairs)))]
	if splitMix64(seed^0xcbbb9d5dc1059ed8)&1 != 0 {
		pair.first, pair.second = pair.second, pair.first
	}
	config.Participants[0].Spawn = pair.first
	config.Participants[1].Spawn = pair.second
	return nil
}

type chainFinisherSetup struct {
	ownerID     uint16
	root        battleengine.Cell
	power       byte
	remainingMS uint32
}

type chainFinisherLayout struct {
	root     battleengine.Cell
	trigger  battleengine.Cell
	attacker battleengine.Cell
	defender battleengine.Cell
}

// applyChainFinisherCurriculum exposes the last two decisions of the standard
// expert conversion without changing any combat rule. The existing root
// bubble reaches trigger; a bubble placed at trigger turns that straight blast
// ninety degrees toward defender. Attacker has a perpendicular retreat, so a
// correct move is dangerous but survivable rather than a scripted sacrifice.
func applyChainFinisherCurriculum(config *battleengine.Config, seed uint64) (chainFinisherSetup, error) {
	if err := applyDuelCurriculum(config, seed); err != nil {
		return chainFinisherSetup{}, err
	}
	if config.Rules.BombFuseMS < 800 || config.Rules.StartClockMS < config.Rules.BombFuseMS-600 {
		return chainFinisherSetup{}, fmt.Errorf(
			"native clock/fuse %d/%d cannot stage a visible late bubble",
			config.Rules.StartClockMS, config.Rules.BombFuseMS,
		)
	}
	open := func(cell battleengine.Cell) bool {
		tile, inside := config.Grid.Cell(cell)
		return inside && tile.Kind == battleengine.CellOpen && !tile.MapElementOccupied
	}
	directions := [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	var preferred, fallback []chainFinisherLayout
	for index, tile := range config.Grid.Cells {
		if tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
			continue
		}
		trigger := battleengine.Cell{Row: int16(index / int(config.Grid.Width)), Col: int16(index % int(config.Grid.Width))}
		for directionIndex, rootDelta := range directions {
			for _, perpendicularOffset := range [...]int{-1, 1} {
				perpendicular := directions[(directionIndex+perpendicularOffset+len(directions))%len(directions)]
				layout := chainFinisherLayout{
					root:     battleengine.Cell{Row: trigger.Row - rootDelta.Row, Col: trigger.Col - rootDelta.Col},
					trigger:  trigger,
					attacker: battleengine.Cell{Row: trigger.Row - perpendicular.Row, Col: trigger.Col - perpendicular.Col},
					defender: battleengine.Cell{Row: trigger.Row + perpendicular.Row, Col: trigger.Col + perpendicular.Col},
				}
				retreat := battleengine.Cell{Row: layout.attacker.Row - perpendicular.Row, Col: layout.attacker.Col - perpendicular.Col}
				if !open(layout.root) || !open(layout.attacker) || !open(layout.defender) || !open(retreat) {
					continue
				}
				fallback = append(fallback, layout)
				if openNeighborCount(config.Grid, layout.defender) <= 2 {
					preferred = append(preferred, layout)
				}
			}
		}
	}
	layouts := preferred
	if len(layouts) == 0 {
		layouts = fallback
	}
	if len(layouts) == 0 {
		return chainFinisherSetup{}, fmt.Errorf("cleared map has no perpendicular chain-conversion layout")
	}
	layout := layouts[int(splitMix64(seed^0x243f6a8885a308d3)%uint64(len(layouts)))]
	attackerIndex := int(splitMix64(seed^0x13198a2e03707344) & 1)
	defenderIndex := 1 - attackerIndex
	config.Participants[attackerIndex].Spawn = layout.attacker
	config.Participants[defenderIndex].Spawn = layout.defender
	attacker := config.Participants[attackerIndex]
	remainingMS := uint32(600 + splitMix64(seed^0xa4093822299f31d0)%401)
	if remainingMS >= config.Rules.BombFuseMS {
		remainingMS = config.Rules.BombFuseMS - 1
	}
	return chainFinisherSetup{
		ownerID:     attacker.PlayerID,
		root:        layout.root,
		power:       attacker.BombPower,
		remainingMS: remainingMS,
	}, nil
}

const bombEscapeMaximumPathCells = 4

// applyBombEscapeCurriculum starts from the ordinary cleared duel and then
// restricts both spawn positions to cells with a real short path outside that
// actor's own native blast. Without this filter a one-cell corridor can create
// an impossible lesson where every action loses, imposing an artificial
// success-rate ceiling unrelated to policy quality.
func applyBombEscapeCurriculum(config *battleengine.Config, seed uint64, capacity byte) error {
	if err := applyDuelCurriculum(config, seed); err != nil {
		return err
	}
	// Stage one isolates exactly one placement and one escape. Developed duel
	// capacity starts at two and lets an untrained policy surround itself with
	// several bubbles before the first fuse expires, accidentally turning the
	// foundation lesson into the harder multi-bomb blockade task. Power remains
	// varied so the learned route must still respect native blast geometry.
	if err := setCurriculumBombCapacity(config, capacity); err != nil {
		return fmt.Errorf("bomb escape %w", err)
	}
	firstPower := config.Participants[0].BombPower
	secondPower := config.Participants[1].BombPower
	var firstCells, secondCells []battleengine.Cell
	for index, tile := range config.Grid.Cells {
		if tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
			continue
		}
		cell := battleengine.Cell{
			Row: int16(index / int(config.Grid.Width)),
			Col: int16(index % int(config.Grid.Width)),
		}
		if bombEscapeRouteExists(config.Grid, cell, firstPower, bombEscapeMaximumPathCells) {
			firstCells = append(firstCells, cell)
		}
		if bombEscapeRouteExists(config.Grid, cell, secondPower, bombEscapeMaximumPathCells) {
			secondCells = append(secondCells, cell)
		}
	}
	var preferred, fallback []duelSpawnPair
	for _, first := range firstCells {
		for _, second := range secondCells {
			if first == second {
				continue
			}
			distance := absInt(int(first.Row-second.Row)) + absInt(int(first.Col-second.Col))
			if distance < 3 {
				continue
			}
			pair := duelSpawnPair{first: first, second: second}
			fallback = append(fallback, pair)
			if distance <= 6 {
				preferred = append(preferred, pair)
			}
		}
	}
	pairs := preferred
	if len(pairs) == 0 {
		pairs = fallback
	}
	if len(pairs) == 0 {
		return fmt.Errorf("cleared map has no two escapable bomb-course spawns")
	}
	pair := pairs[int(splitMix64(seed^0x1f83d9abfb41bd6b)%uint64(len(pairs)))]
	config.Participants[0].Spawn = pair.first
	config.Participants[1].Spawn = pair.second
	return nil
}

func bombEscapeRouteExists(grid battleengine.Grid, start battleengine.Cell, power byte, maximumSteps int) bool {
	if maximumSteps <= 0 || power == 0 {
		return false
	}
	blast := map[battleengine.Cell]struct{}{start: {}}
	directions := [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	for _, direction := range directions {
		for distance := int16(1); distance <= int16(power); distance++ {
			cell := battleengine.Cell{
				Row: start.Row + direction.Row*distance,
				Col: start.Col + direction.Col*distance,
			}
			tile, inside := grid.Cell(cell)
			if !inside {
				break
			}
			if tile.Kind == battleengine.CellBreakable {
				blast[cell] = struct{}{}
				break
			}
			if !tile.FlamePassable {
				break
			}
			blast[cell] = struct{}{}
		}
	}
	type pathNode struct {
		cell  battleengine.Cell
		steps int
	}
	queue := []pathNode{{cell: start}}
	visited := map[battleengine.Cell]struct{}{start: {}}
	for len(queue) != 0 {
		node := queue[0]
		queue = queue[1:]
		if node.steps != 0 {
			if _, threatened := blast[node.cell]; !threatened {
				return true
			}
		}
		if node.steps >= maximumSteps {
			continue
		}
		for _, direction := range directions {
			next := battleengine.Cell{Row: node.cell.Row + direction.Row, Col: node.cell.Col + direction.Col}
			if _, seen := visited[next]; seen {
				continue
			}
			tile, inside := grid.Cell(next)
			if !inside || tile.Kind != battleengine.CellOpen || tile.MapElementOccupied {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, pathNode{cell: next, steps: node.steps + 1})
		}
	}
	return false
}

func developedDuelAttribute(base, maximum byte, seed uint64, minimum byte) byte {
	if maximum <= base {
		return base
	}
	value := base + byte(1+splitMix64(seed)%uint64(maximum-base))
	if value < minimum && maximum >= minimum {
		value = minimum
	}
	return value
}

func (batch *Batch) selectEpisodeMap(seed uint64, spawnMode battleengine.NativeSpawnMode, teamIDs []uint8) (mapdata.CompetitiveMap, error) {
	start := int(seed % uint64(len(batch.maps)))
	for offset := range batch.maps {
		entry := batch.maps[(start+offset)%len(batch.maps)]
		if err := battleengine.ValidateNativeSpawnTopology(entry, spawnMode, teamIDs); err == nil {
			return entry, nil
		}
	}
	return mapdata.CompetitiveMap{}, fmt.Errorf("no training map supports native spawn mode %d for teams %v", spawnMode, teamIDs)
}

func (batch *Batch) Observe() (TensorBatch, error) {
	if batch == nil {
		return TensorBatch{}, fmt.Errorf("training batch is nil")
	}
	var tensors TensorBatch
	if batch.config.ReuseTensorBuffers {
		if batch.tensors.EnvCount == 0 {
			batch.tensors = newTensorBatch(len(batch.episodes), batch.config.ParticipantCount, batch.maxHeight, batch.maxWidth)
		} else {
			clearTensorBatch(&batch.tensors)
		}
		tensors = batch.tensors
	} else {
		tensors = newTensorBatch(len(batch.episodes), batch.config.ParticipantCount, batch.maxHeight, batch.maxWidth)
	}
	indices := make([]int, len(batch.episodes))
	for index := range indices {
		indices[index] = index
	}
	if err := batch.parallelIndices(indices, func(envIndex int) error {
		episode := &batch.episodes[envIndex]
		copy(tensors.TeamIDs[envIndex*batch.config.ParticipantCount:], episode.teamIDs)
		danger, err := episode.engine.DangerTimeline(batch.config.DangerHorizonMS)
		if err != nil {
			return fmt.Errorf("environment %d danger timeline: %w", envIndex, err)
		}
		observations, err := episode.engine.Observations(episode.playerIDs)
		if err != nil {
			return fmt.Errorf("environment %d observations: %w", envIndex, err)
		}
		for actorIndex, playerID := range episode.playerIDs {
			legal, legalErr := episode.engine.LegalActionMask(playerID)
			if legalErr != nil {
				return fmt.Errorf("environment %d legal player %d: %w", envIndex, playerID, legalErr)
			}
			if err := encodeActor(&tensors, envIndex, actorIndex, observations[actorIndex], danger, legal); err != nil {
				return fmt.Errorf("environment %d encode player %d: %w", envIndex, playerID, err)
			}
		}
		return nil
	}); err != nil {
		return TensorBatch{}, err
	}
	return tensors, nil
}

func balancedNativeTeamLayout(teamIDs []uint8) bool {
	if len(teamIDs) == 0 || len(teamIDs)%2 != 0 {
		return false
	}
	counts := make(map[uint8]int, 2)
	for _, teamID := range teamIDs {
		counts[teamID]++
	}
	if len(counts) != 2 {
		return false
	}
	expected := len(teamIDs) / 2
	for _, count := range counts {
		if count != expected {
			return false
		}
	}
	return true
}

func (batch *Batch) parallelIndices(indices []int, operation func(int) error) error {
	if len(indices) == 0 {
		return nil
	}
	workers := batch.workers
	if workers <= 1 || len(indices) == 1 {
		for _, envIndex := range indices {
			if err := operation(envIndex); err != nil {
				return err
			}
		}
		return nil
	}
	if workers > len(indices) {
		workers = len(indices)
	}
	errorsByIndex := make([]error, len(indices))
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := worker; index < len(indices); index += workers {
				errorsByIndex[index] = operation(indices[index])
			}
		}(worker)
	}
	wait.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			return err
		}
	}
	return nil
}

func validateTeamLayout(layout []uint8, participantCount int) error {
	if len(layout) != participantCount {
		return fmt.Errorf("has %d participants, want %d", len(layout), participantCount)
	}
	seen := make(map[uint8]struct{}, participantCount)
	for _, teamID := range layout {
		if teamID == 0 || int(teamID) > participantCount {
			return fmt.Errorf("team ID %d is outside 1..%d", teamID, participantCount)
		}
		seen[teamID] = struct{}{}
	}
	if len(seen) < 2 {
		return fmt.Errorf("must contain at least two teams")
	}
	for teamID := uint8(1); teamID <= uint8(len(seen)); teamID++ {
		if _, ok := seen[teamID]; !ok {
			return fmt.Errorf("team IDs must be contiguous from 1")
		}
	}
	return nil
}

func (batch *Batch) Step(actionIDs [][]battleengine.ActionID) (StepResult, error) {
	if batch == nil {
		return StepResult{}, fmt.Errorf("training batch is nil")
	}
	if len(actionIDs) != len(batch.episodes) {
		return StepResult{}, fmt.Errorf("action environment count %d, want %d", len(actionIDs), len(batch.episodes))
	}
	result := StepResult{
		Rewards: make([]float32, len(batch.episodes)*batch.config.ParticipantCount),
		Dones:   make([]uint8, len(batch.episodes)), Outcomes: make([]battleengine.Outcome, len(batch.episodes)),
		SelfEliminations: make([]uint8, len(batch.episodes)*batch.config.ParticipantCount),
		Events:           make([][]battleengine.Event, len(batch.episodes)),
	}
	initialByEnvironment := make([][]battleengine.Action, len(batch.episodes))
	for envIndex := range batch.episodes {
		episode := &batch.episodes[envIndex]
		if len(actionIDs[envIndex]) != batch.config.ParticipantCount {
			return StepResult{}, fmt.Errorf("environment %d action count %d, want tensor capacity %d", envIndex, len(actionIDs[envIndex]), batch.config.ParticipantCount)
		}
		if episode.engine.Terminal().Ended {
			return StepResult{}, fmt.Errorf("environment %d is terminal and must be reset", envIndex)
		}
		initial := make([]battleengine.Action, len(episode.playerIDs))
		for actorIndex, playerID := range episode.playerIDs {
			action, ok := battleengine.ActionFromID(playerID, actionIDs[envIndex][actorIndex])
			if !ok {
				return StepResult{}, fmt.Errorf("environment %d player %d has invalid action ID %d", envIndex, playerID, actionIDs[envIndex][actorIndex])
			}
			mask, maskErr := episode.engine.LegalActionMask(playerID)
			if maskErr != nil {
				return StepResult{}, maskErr
			}
			if !mask[actionIDs[envIndex][actorIndex]] {
				return StepResult{}, fmt.Errorf("environment %d player %d selected illegal action %d", envIndex, playerID, actionIDs[envIndex][actorIndex])
			}
			initial[actorIndex] = action
		}
		initialByEnvironment[envIndex] = initial
	}
	indices := make([]int, len(batch.episodes))
	for index := range indices {
		indices[index] = index
	}
	if err := batch.parallelIndices(indices, func(envIndex int) error {
		episode := &batch.episodes[envIndex]
		initial := initialByEnvironment[envIndex]
		ticks := int(batch.config.DecisionMS / batch.config.TickMS)
		var bombEscapeWinnerTeam byte
		bombEscapeSucceeded := false
		for tick := 0; tick < ticks && !episode.engine.Terminal().Ended; tick++ {
			actions := make([]battleengine.Action, len(initial))
			for actorIndex, action := range initial {
				actions[actorIndex] = action
				if tick != 0 {
					actions[actorIndex].PlaceBomb = false
					actions[actorIndex].UseActionID = 0
				}
			}
			events, err := episode.engine.Step(actions)
			if err != nil {
				return fmt.Errorf("environment %d tick %d: %w", envIndex, tick, err)
			}
			episode.metrics.Ticks++
			result.Events[envIndex] = append(result.Events[envIndex], events...)
			batch.accumulateEvents(envIndex, events, result.Rewards)
			recordSelfEliminations(
				episode.playerIDs,
				events,
				result.SelfEliminations[envIndex*batch.config.ParticipantCount:(envIndex+1)*batch.config.ParticipantCount],
			)
			batch.accumulateEnemyApproach(envIndex, result.Rewards)
			batch.accumulateLateStallBehavior(envIndex, result.Rewards)
			batch.accumulateBlockedCellBehavior(envIndex, result.Rewards)
			if episode.metrics.BombEscapeCurriculum {
				actors := episode.engine.Actors()
				var completedTeam byte
				for _, ownerID := range safeBombEscapeOwners(events, actors) {
					owner := actorIndexByPlayerID(actors, ownerID)
					if owner < 0 {
						continue
					}
					training := &episode.training[owner]
					if training.safeDetonations < ^byte(0) {
						training.safeDetonations++
					}
					if training.safeDetonations < batch.config.BombEscapeRequiredDetonations {
						continue
					}
					teamID := actors[owner].TeamID
					if completedTeam != 0 && completedTeam != teamID {
						completedTeam = 0
						break
					}
					completedTeam = teamID
				}
				if completedTeam != 0 {
					bombEscapeWinnerTeam = completedTeam
					bombEscapeSucceeded = true
					break
				}
			}
		}
		episode.metrics.Decisions++
		outcome := episode.engine.Terminal()
		if !outcome.Ended && bombEscapeSucceeded {
			outcome = battleengine.Outcome{
				Ended: true, WinnerTeamID: bombEscapeWinnerTeam,
				EndedAtMS: episode.engine.ElapsedMS(),
			}
		}
		result.Outcomes[envIndex] = outcome
		if outcome.Ended {
			result.Dones[envIndex] = 1
			episode.metrics.TimedOut = outcome.TimedOut
			actors := episode.engine.Actors()
			material := materialByTeam(actors)
			for actorIndex, actor := range actors {
				rewardIndex := envIndex*batch.config.ParticipantCount + actorIndex
				result.Rewards[rewardIndex] += terminalOutcomeReward(actor.TeamID, outcome, material)
			}
		}
		return nil
	}); err != nil {
		return StepResult{}, err
	}
	observation, err := batch.Observe()
	if err != nil {
		return StepResult{}, err
	}
	result.Observation = observation
	return result, nil
}

func actorIndexByPlayerID(actors []battleengine.Actor, playerID uint16) int {
	for index, actor := range actors {
		if actor.PlayerID == playerID {
			return index
		}
	}
	return -1
}

// safeBombEscapeOwners recognizes authoritative completed escapes, not
// predicted safe actions. A bomb must actually explode and its owner must
// still be active after the engine has applied every blast effect in the tick.
func safeBombEscapeOwners(events []battleengine.Event, actors []battleengine.Actor) []uint16 {
	activePlayers := make(map[uint16]struct{}, len(actors))
	for _, actor := range actors {
		if actor.State == battleengine.ActorActive {
			activePlayers[actor.PlayerID] = struct{}{}
		}
	}
	seen := make(map[uint16]struct{})
	owners := make([]uint16, 0, len(events))
	for _, event := range events {
		if event.Kind != battleengine.EventBombExploded {
			continue
		}
		_, active := activePlayers[event.PlayerID]
		if !active {
			continue
		}
		if _, duplicate := seen[event.PlayerID]; duplicate {
			continue
		}
		seen[event.PlayerID] = struct{}{}
		owners = append(owners, event.PlayerID)
	}
	return owners
}

func recordSelfEliminations(playerIDs []uint16, events []battleengine.Event, result []uint8) {
	for _, event := range events {
		if event.Kind != battleengine.EventActorEliminated || event.PlayerID != event.TargetID {
			continue
		}
		for actorIndex, playerID := range playerIDs {
			if playerID == event.TargetID && actorIndex < len(result) {
				if result[actorIndex] < ^uint8(0) {
					result[actorIndex]++
				}
				break
			}
		}
	}
}

func (batch *Batch) accumulateEnemyApproach(envIndex int, rewards []float32) {
	episode := &batch.episodes[envIndex]
	actors := episode.engine.Actors()
	for actorIndex, actor := range actors {
		training := &episode.training[actorIndex]
		if actor.State == battleengine.ActorEliminated {
			training.hasEnemyDistance = false
			continue
		}
		nearest := -1
		cell := actor.Position.Cell()
		for _, candidate := range actors {
			if candidate.State == battleengine.ActorEliminated || candidate.TeamID == actor.TeamID {
				continue
			}
			other := candidate.Position.Cell()
			distance := absInt(int(cell.Row-other.Row)) + absInt(int(cell.Col-other.Col))
			if nearest < 0 || distance < nearest {
				nearest = distance
			}
		}
		if nearest < 0 {
			training.hasEnemyDistance = false
			continue
		}
		// Keep the baseline current while trapped, but do not pay movement
		// shaping. Clearing it here allowed approach -> trap -> rescue cycles to
		// earn the same approach potential repeatedly without moving away.
		if actor.State == battleengine.ActorActive && training.hasEnemyDistance {
			rewards[envIndex*batch.config.ParticipantCount+actorIndex] += enemyApproachReward(
				episode.engine.RoundElapsedMS(), training.enemyDistance, nearest,
			)
		}
		training.enemyDistance = nearest
		training.hasEnemyDistance = true
	}
}

func enemyApproachReward(elapsedMS uint32, previousDistance, currentDistance int) float32 {
	if elapsedMS < rewardDevelopmentEndMS {
		return 0
	}
	return float32(previousDistance-currentDistance) * rewardEnemyApproachPerCell
}

func developmentProgressReward(elapsedMS uint32, base, openingBonus float32) float32 {
	if elapsedMS < rewardDevelopmentEndMS {
		return base + openingBonus
	}
	return base
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// blockadeAssistOwners attributes only immediate, observable escape denial.
// An assistant must own either a bubble occupying one of the victim's four
// neighboring cells or a distinct flame that impacted such a cell in the same
// engine tick. Distant future blast rays are intentionally excluded: they may
// look threatening but did not physically close the route that produced this
// trap. The returned owner list is unique and stable.
func blockadeAssistOwners(engine *battleengine.Engine, actors []battleengine.Actor, targetIndex int, directOwnerID uint16) []uint16 {
	if engine == nil || targetIndex < 0 || targetIndex >= len(actors) {
		return nil
	}
	target := actors[targetIndex]
	neighbors := make(map[battleengine.Cell]struct{}, 4)
	for _, direction := range [...]battleengine.Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}} {
		cell := battleengine.Cell{Row: target.Position.Cell().Row + direction.Row, Col: target.Position.Cell().Col + direction.Col}
		if _, inside := engine.TileAt(cell); inside {
			neighbors[cell] = struct{}{}
		}
	}
	ownerTeam := func(playerID uint16) (uint8, bool) {
		for _, actor := range actors {
			if actor.PlayerID == playerID {
				return actor.TeamID, true
			}
		}
		return 0, false
	}
	owners := make(map[uint16]struct{})
	accept := func(playerID uint16) {
		if playerID == 0 || playerID == directOwnerID {
			return
		}
		teamID, ok := ownerTeam(playerID)
		if !ok || teamID == target.TeamID {
			return
		}
		owners[playerID] = struct{}{}
	}
	for _, bomb := range engine.Bombs() {
		if _, adjacent := neighbors[bomb.Cell]; adjacent {
			accept(bomb.OwnerID)
		}
	}
	for _, flame := range engine.Flames() {
		if flame.ImpactAtMS != engine.ElapsedMS() {
			continue
		}
		if _, adjacent := neighbors[flame.Cell]; adjacent {
			accept(flame.OwnerID)
		}
	}
	result := make([]uint16, 0, len(owners))
	for playerID := range owners {
		result = append(result, playerID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// SearchTeacher reranks the actor's visible-state logits with bounded tactical
// simulation. It is an offline demonstration/evaluation path: the live server
// defaults to one greedy actor inference and never calls this method.
func (batch *Batch) SearchTeacher(logits []float32, config battleengine.SearchConfig) ([][]battleengine.ActionID, error) {
	result, err := batch.SearchTeacherScores(logits, config)
	if err != nil {
		return nil, err
	}
	return result.Actions, nil
}

// SearchTeacherResult preserves the chosen action and the scored Top-K set.
// Status is zero for unevaluated actions, one for evaluated unsafe actions and
// two for evaluated actions that survive the bounded tactical rollout.
type SearchTeacherResult struct {
	Actions  [][]battleengine.ActionID
	Values   []float32
	Statuses []uint8
}

// SearchTeacherScores exposes the multi-action Search value distribution for
// offline distillation without changing the live one-action policy contract.
func (batch *Batch) SearchTeacherScores(logits []float32, config battleengine.SearchConfig) (*SearchTeacherResult, error) {
	if batch == nil {
		return nil, fmt.Errorf("training batch is nil")
	}
	actionCount := int(battleengine.DiscreteActionCount)
	expected := len(batch.episodes) * batch.config.ParticipantCount * actionCount
	if len(logits) != expected {
		return nil, fmt.Errorf("search-teacher logit count %d, want %d", len(logits), expected)
	}
	config.AlwaysSearch = true
	result := &SearchTeacherResult{
		Actions:  make([][]battleengine.ActionID, len(batch.episodes)),
		Values:   make([]float32, expected),
		Statuses: make([]uint8, expected),
	}
	indices := make([]int, len(batch.episodes))
	for index := range indices {
		indices[index] = index
		result.Actions[index] = make([]battleengine.ActionID, batch.config.ParticipantCount)
	}
	err := batch.parallelIndices(indices, func(envIndex int) error {
		episode := &batch.episodes[envIndex]
		for actorIndex, playerID := range episode.playerIDs {
			observation, err := episode.engine.Observation(playerID)
			if err != nil {
				return fmt.Errorf("environment %d observe player %d: %w", envIndex, playerID, err)
			}
			legal, err := episode.engine.LegalActions(playerID)
			if err != nil {
				return fmt.Errorf("environment %d legal player %d: %w", envIndex, playerID, err)
			}
			candidates := make(fixedSearchCandidates, 0, len(legal))
			base := (envIndex*batch.config.ParticipantCount + actorIndex) * actionCount
			for _, action := range legal {
				actionID, ok := action.ID()
				if !ok {
					return fmt.Errorf("environment %d player %d legal action has no stable ID", envIndex, playerID)
				}
				score := logits[base+int(actionID)]
				// The Python safety layer uses a large negative sentinel for
				// actions it intentionally excludes. Do not refill Top-K with
				// those actions merely because an actor has few safe choices.
				if score <= -1e8 {
					continue
				}
				candidates = append(candidates, battleengine.ScoredAction{Action: action, Score: score})
			}
			if len(candidates) == 0 {
				return fmt.Errorf("environment %d player %d has no search candidates after safety mask", envIndex, playerID)
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				if candidates[i].Score == candidates[j].Score {
					left, _ := candidates[i].Action.ID()
					right, _ := candidates[j].Action.ID()
					return left < right
				}
				return candidates[i].Score > candidates[j].Score
			})
			snapshot, err := episode.engine.PolicySnapshot(playerID)
			if err != nil {
				return fmt.Errorf("environment %d policy snapshot player %d: %w", envIndex, playerID, err)
			}
			policy := battleengine.TopKSearchPolicy{Candidate: candidates, Config: config}
			ranked, err := policy.RankActionsWithSnapshot(snapshot, observation, legal)
			if err != nil {
				return fmt.Errorf("environment %d search player %d: %w", envIndex, playerID, err)
			}
			if len(ranked) == 0 {
				return fmt.Errorf("environment %d search player %d returned no ranked action", envIndex, playerID)
			}
			for _, scored := range ranked {
				actionID, ok := scored.Action.ID()
				if !ok {
					return fmt.Errorf("environment %d search player %d returned unstable scored action", envIndex, playerID)
				}
				offset := base + int(actionID)
				result.Values[offset] = float32(scored.Value)
				result.Statuses[offset] = 1
				if scored.Survives {
					result.Statuses[offset] = 2
				}
			}
			actionID, ok := ranked[0].Action.ID()
			if !ok {
				return fmt.Errorf("environment %d search player %d returned unstable action", envIndex, playerID)
			}
			result.Actions[envIndex][actorIndex] = actionID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (batch *Batch) accumulateEvents(envIndex int, events []battleengine.Event, rewards []float32) {
	episode := &batch.episodes[envIndex]
	actors := episode.engine.Actors()
	actorIndex := func(playerID uint16) int {
		for index, candidate := range episode.playerIDs {
			if candidate == playerID {
				return index
			}
		}
		return -1
	}
	add := func(playerID uint16, amount float32) bool {
		if index := actorIndex(playerID); index >= 0 {
			rewards[envIndex*batch.config.ParticipantCount+index] += amount
			return true
		}
		return false
	}
	applyDelta := func(deltas *[]rewardDelta, playerID uint16, amount float32) {
		if amount != 0 && add(playerID, amount) && deltas != nil {
			*deltas = append(*deltas, rewardDelta{playerID: playerID, amount: amount})
		}
	}
	addCombatResult := func(owner, target int, amount float32, deltas *[]rewardDelta) {
		if owner >= 0 && target >= 0 && owner != target {
			if actors[owner].TeamID == actors[target].TeamID {
				applyDelta(deltas, actors[owner].PlayerID, -amount*rewardFriendlyFireFactor)
			} else {
				applyDelta(deltas, actors[owner].PlayerID, amount)
			}
		}
		if target >= 0 {
			applyDelta(deltas, actors[target].PlayerID, -amount)
		}
	}
	markProgress := func(owner int, combat bool) {
		if owner >= 0 {
			elapsed := episode.engine.RoundElapsedMS()
			// Terrain and pickups are useful early development, but after the
			// midpoint only an actual interaction with another team postpones
			// the escalating anti-stall cost.
			if combat || elapsed < rewardLateStallStartMS {
				episode.teamProgress[actors[owner].TeamID] = elapsed
			}
		}
	}
	isEnemyResult := func(owner, target int) bool {
		return owner >= 0 && target >= 0 && owner != target && actors[owner].TeamID != actors[target].TeamID
	}
	teamImpactScale := func(target int) float32 {
		if target < 0 || target >= len(actors) {
			return 1
		}
		members := 0
		for _, actor := range actors {
			if actor.TeamID == actors[target].TeamID {
				members++
			}
		}
		if members <= 1 {
			return 1
		}
		return 1 / float32(members)
	}
	rollbackTrap := func(targetID uint16) (trapRewardCredit, bool) {
		credit, ok := episode.trapCredits[targetID]
		if !ok {
			return trapRewardCredit{}, false
		}
		for _, delta := range credit.deltas {
			add(delta.playerID, -delta.amount)
		}
		delete(episode.trapCredits, targetID)
		return credit, true
	}
	for _, event := range events {
		owner := actorIndex(event.PlayerID)
		target := actorIndex(event.TargetID)
		switch event.Kind {
		case battleengine.EventActorMoved:
			if owner >= 0 {
				episode.metrics.Agents[owner].MovedTicks++
			}
		case battleengine.EventBombPlaced:
			if owner >= 0 {
				episode.metrics.Agents[owner].BombsPlaced++
			}
			add(event.PlayerID, rewardBombPlaced)
		case battleengine.EventCellDestroyed:
			if owner >= 0 {
				episode.metrics.Agents[owner].WallsDestroyed++
			}
			add(event.PlayerID, developmentProgressReward(
				episode.engine.RoundElapsedMS(), rewardWallDestroyed, rewardDevelopmentWallBonus,
			))
			markProgress(owner, false)
		case battleengine.EventPickupCollected:
			if owner >= 0 {
				episode.metrics.Agents[owner].Pickups++
			}
			add(event.PlayerID, developmentProgressReward(
				episode.engine.RoundElapsedMS(), rewardPickup, rewardDevelopmentPickupBonus,
			))
			markProgress(owner, false)
		case battleengine.EventFieldObjectTriggered:
			if event.ActionID == 42 || event.ActionID == 43 {
				addCombatResult(owner, target, rewardMovementDebuff*teamImpactScale(target), nil)
				if isEnemyResult(owner, target) {
					markProgress(owner, true)
				}
			}
		case battleengine.EventActorTrapped:
			if owner >= 0 {
				episode.metrics.Agents[owner].Traps++
			}
			// A duplicated/reconciled hit must replace, not stack with, an
			// unresolved credit for the same target.
			rollbackTrap(event.TargetID)
			credit := trapRewardCredit{enemyCaused: isEnemyResult(owner, target)}
			impactScale := teamImpactScale(target)
			addCombatResult(owner, target, rewardTrap*impactScale, &credit.deltas)
			if credit.enemyCaused {
				markProgress(owner, true)
				assistants := blockadeAssistOwners(episode.engine, actors, target, event.PlayerID)
				if len(assistants) != 0 {
					share := rewardTrapAssistTotal * impactScale / float32(len(assistants))
					for _, assistantID := range assistants {
						applyDelta(&credit.deltas, assistantID, share)
						assistant := actorIndex(assistantID)
						if assistant >= 0 {
							episode.metrics.Agents[assistant].TrapAssists++
							markProgress(assistant, true)
						}
					}
				}
			}
			if target >= 0 {
				episode.trapCredits[event.TargetID] = credit
			}
		case battleengine.EventActorEliminated:
			// Elimination confirms the pending trap advantage; keep the
			// deltas but remove the rollback handle before awarding the kill.
			delete(episode.trapCredits, event.TargetID)
			if owner >= 0 {
				episode.metrics.Agents[owner].Eliminations++
			}
			if owner >= 0 && owner == target {
				episode.metrics.Agents[owner].SelfEliminations++
			}
			addCombatResult(owner, target, rewardElimination*teamImpactScale(target), nil)
			if isEnemyResult(owner, target) {
				markProgress(owner, true)
			}
		case battleengine.EventActorRescued:
			credit, hadTrap := rollbackTrap(event.TargetID)
			validTeamRescue := owner >= 0 && target >= 0 && actors[owner].TeamID == actors[target].TeamID
			forkSelfRescue := event.ActionID == 63 && event.PlayerID == event.TargetID
			// Genuine enemy-caused rescue (or a finite fork self-rescue) spends
			// half of its remaining bounded budget. Friendly trap/rescue
			// cooperation earns nothing, while repeated useful rescues remain
			// learnable with 0.20, 0.10, 0.05... diminishing credit.
			if forkSelfRescue || (hadTrap && credit.enemyCaused && validTeamRescue) {
				paid := episode.rescueRewardPaid[event.TargetID]
				budget := rewardRescueBudget * teamImpactScale(target)
				bonus := (budget - paid) / 2
				if bonus > 0 {
					add(event.PlayerID, bonus)
					episode.rescueRewardPaid[event.TargetID] = paid + bonus
				}
				if owner >= 0 {
					episode.metrics.Agents[owner].Rescues++
				}
				markProgress(owner, true)
			}
		case battleengine.EventActorTransformationEnded:
			if event.TransformationEnd == battleengine.TransformationEndHit {
				// Transformation events name the victim in PlayerID and the
				// attacking source in TargetID, opposite to trap/elimination.
				addCombatResult(target, owner, rewardTransformationBroken*teamImpactScale(owner), nil)
				if isEnemyResult(target, owner) {
					markProgress(target, true)
				}
			}
		case battleengine.EventBattleActionUsed:
			if owner >= 0 {
				episode.metrics.Agents[owner].ActionsUsed++
			}
		case battleengine.EventNativePassStarted:
			if owner >= 0 {
				episode.metrics.Agents[owner].NativePasses++
			}
		}
	}
}

func lateStallPenalty(elapsedMS, lastProgressMS, tickMS uint32) float32 {
	if elapsedMS < rewardLateStallStartMS || elapsedMS-lastProgressMS <= rewardLateStallProgressGraceMS {
		return 0
	}
	lateWindow := rewardRoundDurationMS - rewardLateStallStartMS
	lateElapsed := elapsedMS - rewardLateStallStartMS
	if lateElapsed > lateWindow {
		lateElapsed = lateWindow
	}
	phase := float32(lateElapsed) / float32(lateWindow)
	multiplier := 1 + phase*(rewardLateStallMaxMultiplier-1)
	return -rewardLateStallPenaltyPerSecond * multiplier * float32(tickMS) / 1_000
}

func lateStallResponsibility(teamID byte, actors []battleengine.Actor) float32 {
	teamLive := 0
	opponentLive := 0
	for _, actor := range actors {
		if actor.State == battleengine.ActorEliminated {
			continue
		}
		if actor.TeamID == teamID {
			teamLive++
		} else {
			opponentLive++
		}
	}
	if teamLive < opponentLive {
		return 0
	}
	totalLive := teamLive + opponentLive
	if totalLive <= 0 {
		return 0
	}
	advantage := float32(teamLive-opponentLive) / float32(totalLive)
	return 1 + advantage
}

func (batch *Batch) accumulateLateStallBehavior(envIndex int, rewards []float32) {
	episode := &batch.episodes[envIndex]
	elapsedMS := episode.engine.RoundElapsedMS()
	actors := episode.engine.Actors()
	for actorIndex, actor := range actors {
		if actor.State != battleengine.ActorActive {
			continue
		}
		responsibility := lateStallResponsibility(actor.TeamID, actors)
		penalty := responsibility * lateStallPenalty(elapsedMS, episode.teamProgress[actor.TeamID], batch.config.TickMS)
		if penalty == 0 {
			continue
		}
		episode.metrics.Agents[actorIndex].LateStallTicks++
		rewards[envIndex*batch.config.ParticipantCount+actorIndex] += penalty
	}
}

func (batch *Batch) accumulateBlockedCellBehavior(envIndex int, rewards []float32) {
	episode := &batch.episodes[envIndex]
	actors := episode.engine.Actors()
	graceTicks := (rewardBlockedCellGraceMS + batch.config.TickMS - 1) / batch.config.TickMS
	penaltyPerTick := rewardBlockedCellPenaltyPerSecond * float32(batch.config.TickMS) / 1_000
	for actorIndex, actor := range actors {
		training := &episode.training[actorIndex]
		if actor.State != battleengine.ActorActive {
			training.blocked = false
			training.blockedTicks = 0
			continue
		}
		tile, inside := episode.engine.TileAt(actor.Position.Cell())
		if !inside || tile.Kind == battleengine.CellOpen {
			if training.blocked {
				training.hasBlockedExit = true
				training.lastBlockedExitMS = episode.engine.ElapsedMS()
			}
			training.blocked = false
			training.blockedTicks = 0
			continue
		}
		if !training.blocked {
			training.blocked = true
			metrics := &episode.metrics.Agents[actorIndex]
			metrics.BlockedEntries++
			rewardIndex := envIndex*batch.config.ParticipantCount + actorIndex
			if metrics.BlockedEntries > rewardFreeBlockedEntries {
				penalty := float32(metrics.BlockedEntries-rewardFreeBlockedEntries) * rewardRepeatedBlockedEntryPenalty
				if penalty > rewardBlockedEntryPenaltyCap {
					penalty = rewardBlockedEntryPenaltyCap
				}
				rewards[rewardIndex] -= penalty
			}
			if training.hasBlockedExit && episode.engine.ElapsedMS()-training.lastBlockedExitMS < rewardBlockedReentryWindowMS {
				rewards[rewardIndex] -= rewardRapidBlockedReentryPenalty
			}
		}
		training.blockedTicks++
		episode.metrics.Agents[actorIndex].BlockedTicks++
		if training.blockedTicks <= graceTicks {
			continue
		}
		rewardIndex := envIndex*batch.config.ParticipantCount + actorIndex
		rewards[rewardIndex] -= penaltyPerTick
	}
}

func (batch *Batch) Metrics() []EpisodeMetrics {
	if batch == nil {
		return nil
	}
	result := make([]EpisodeMetrics, len(batch.episodes))
	for index := range batch.episodes {
		result[index] = batch.episodes[index].metrics
		result[index].Agents = append([]AgentMetrics(nil), batch.episodes[index].metrics.Agents...)
	}
	return result
}

func (batch *Batch) MapIDs() []uint32 {
	if batch == nil {
		return nil
	}
	result := make([]uint32, len(batch.episodes))
	for index := range batch.episodes {
		result[index] = batch.episodes[index].metrics.MapID
	}
	return result
}

func splitMix64(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}
