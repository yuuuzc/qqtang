package game

import "fmt"

// CompetitiveRank is one of the 180 client rank steps. Six adjacent steps
// share a title and main medal while SubLevel selects the distinct tie icon.
type CompetitiveRank struct {
	Degree    uint16 `json:"degree"`
	Title     string `json:"title"`
	MainLevel byte   `json:"main_level"`
	SubLevel  byte   `json:"sub_level"`
	MinPoints uint32 `json:"min_points"`
	MaxPoints uint32 `json:"max_points"`
}

// AdventureRank is derived entirely from GAME_INFO.ExtPoint. The old client
// has no separate adventure-level field on the wire.
type AdventureRank struct {
	Level     byte   `json:"level"`
	MinPoints uint32 `json:"min_points"`
	MaxPoints uint32 `json:"max_points"`
}

// ProgressionChange describes one bounded experience mutation. Level is the
// competitive Degree or the adventure level derived from the original client
// tables. AppliedPoints can be smaller than EarnedPoints when the client cap is
// reached.
type ProgressionChange struct {
	PreviousPoints uint32 `json:"previous_points"`
	CurrentPoints  uint32 `json:"current_points"`
	EarnedPoints   uint32 `json:"earned_points"`
	AppliedPoints  uint32 `json:"applied_points"`
	PreviousLevel  uint16 `json:"previous_level"`
	CurrentLevel   uint16 `json:"current_level"`
	LeveledUp      bool   `json:"leveled_up"`
	Capped         bool   `json:"capped"`
}

// competitivePointThresholds and competitiveRankTitles are the constants in
// the shipped res/uiRes/levelCFG.pyc (SHA-256
// 9170CD4C0E9B686C14E131290BE67A2A906974D6828EF69EF669252016022656).
// The decompiled source loses these large integer constants, so this table was
// read from the Python 2.3 code object's constant pool.
var competitivePointThresholds = [...]uint32{
	0, 1000, 2000, 3000, 4000, 5000, 10000, 12000, 14000, 16000, 18000, 20000,
	30000, 33000, 36000, 39000, 42000, 45000, 60000, 64000, 68000, 72000, 76000,
	80000, 100000, 105000, 110000, 115000, 120000, 125000, 150000, 156000, 162000,
	168000, 174000, 180000, 210000, 217000, 224000, 231000, 238000, 245000, 280000,
	288000, 296000, 304000, 312000, 320000, 360000, 369000, 378000, 387000, 396000,
	405000, 450000, 460000, 470000, 480000, 490000, 500000, 550000, 565000, 580000,
	595000, 610000, 625000, 700000, 720000, 740000, 760000, 780000, 800000, 900000,
	925000, 950000, 975000, 1000000, 1025000, 1150000, 1180000, 1210000, 1240000,
	1270000, 1300000, 1450000, 1485000, 1520000, 1555000, 1590000, 1625000, 1800000,
	1840000, 1880000, 1920000, 1960000, 2000000, 2200000, 2245000, 2290000, 2335000,
	2380000, 2425000, 2650000, 2700000, 2750000, 2800000, 2850000, 2900000, 3150000,
	3205000, 3260000, 3315000, 3370000, 3425000, 3700000, 3760000, 3820000, 3880000,
	3940000, 4000000, 4300000, 4370000, 4440000, 4510000, 4580000, 4650000, 5000000,
	5090000, 5180000, 5270000, 5360000, 5450000, 5900000, 6020000, 6140000, 6260000,
	6380000, 6500000, 7100000, 7260000, 7420000, 7580000, 7740000, 7900000, 8700000,
	8910000, 9120000, 9330000, 9540000, 9750000, 10800000, 11070000, 11340000,
	11610000, 11880000, 12150000, 13500000, 14685000, 15870000, 17055000, 18240000,
	19425000, 25350000, 29145000, 32940000, 36735000, 40530000, 44325000, 63300000,
	71380000, 79460000, 87540000, 95620000, 103700000, 144100000, 292805000, 441510000,
	590515000, 738920000, 887625000, 1631150000,
}

var competitiveRankTitles = [...]string{
	"QQ堂平民", "糖果爱好者", "采购学徒", "熬糖工人", "拌糖熟练工", "甜味技术员",
	"捏形师傅", "品糖高手", "造型专家", "QQ糖大师", "糖果志愿者", "奶酪预备兵",
	"QQ糖战士", "刨冰骑兵", "棒棒糖游侠", "饼干队长", "果冻骑士", "棒棒糖将军",
	"冰琪淋勇者", "QQ堂英雄", "糖果镇长", "中华城主", "梦幻岛主", "星星公爵",
	"月亮王子", "太阳国王", "紫钻皇帝", "酷比大天使", "创世之神", "???",
}

// adventurePointThresholds are the exact point attributes from the client's
// expanded config/pvedata.dat, including level zero and the final level 30.
var adventurePointThresholds = [...]uint32{
	0, 573, 1289, 2447, 4481, 7952, 20000, 36100, 60000, 96000, 154000, 231000,
	323401, 431858, 565350, 790000, 1040000, 1430000, 1880000, 2420000, 3010000,
	5322310, 9070163, 15066729, 24661235, 40012444, 64574378, 103873473,
	166752024, 267357707, 428326799,
}

var (
	competitiveRanks = buildCompetitiveRanks()
	adventureRanks   = buildAdventureRanks()
)

func buildCompetitiveRanks() []CompetitiveRank {
	ranks := make([]CompetitiveRank, MaxPlayerLevel)
	for index := range ranks {
		degree := index + 1
		maximum := competitivePointThresholds[degree] - 1
		if degree == MaxPlayerLevel {
			maximum = MaxPlayerExperience
		}
		ranks[index] = CompetitiveRank{
			Degree: uint16(degree), Title: competitiveRankTitles[index/6],
			MainLevel: byte(index/6 + 1), SubLevel: byte(index%6 + 1),
			MinPoints: competitivePointThresholds[index], MaxPoints: maximum,
		}
	}
	return ranks
}

func buildAdventureRanks() []AdventureRank {
	ranks := make([]AdventureRank, len(adventurePointThresholds))
	for index := range ranks {
		maximum := uint32(MaxAdventureExperience)
		if index+1 < len(adventurePointThresholds) {
			maximum = adventurePointThresholds[index+1] - 1
		}
		ranks[index] = AdventureRank{Level: byte(index), MinPoints: adventurePointThresholds[index], MaxPoints: maximum}
	}
	return ranks
}

func CompetitiveRanks() []CompetitiveRank {
	return append([]CompetitiveRank(nil), competitiveRanks...)
}

func CompetitiveRankByDegree(degree uint16) (CompetitiveRank, error) {
	if degree < 1 || degree > MaxPlayerLevel {
		return CompetitiveRank{}, fmt.Errorf("competitive degree %d is outside 1..%d", degree, MaxPlayerLevel)
	}
	return competitiveRanks[degree-1], nil
}

func CompetitiveRankForPoints(points uint32) CompetitiveRank {
	for degree := 1; degree < len(competitivePointThresholds); degree++ {
		if points < competitivePointThresholds[degree] {
			return competitiveRanks[degree-1]
		}
	}
	return competitiveRanks[len(competitiveRanks)-1]
}

func AdventureRanks() []AdventureRank {
	return append([]AdventureRank(nil), adventureRanks...)
}

func AdventureRankForPoints(points uint32) AdventureRank {
	for level := 1; level < len(adventurePointThresholds); level++ {
		if points < adventurePointThresholds[level] {
			return adventureRanks[level-1]
		}
	}
	return adventureRanks[len(adventureRanks)-1]
}

// WithDegreeDerivedFromPoints keeps the explicit protocol Degree field in
// lockstep with the point table used by the client UI.
func (info GameInfo) WithDegreeDerivedFromPoints() GameInfo {
	info.Degree = CompetitiveRankForPoints(info.Point).Degree
	return info
}

// ApplyCompetitiveExperience applies points using the exact 180-step client
// table. Degree is persisted in GAME_INFO, so it is always re-derived even
// when the award is zero or the maximum has already been reached.
func ApplyCompetitiveExperience(info GameInfo, earned uint32) (GameInfo, ProgressionChange) {
	previous := minUint32(info.Point, MaxPlayerExperience)
	current := boundedExperienceAdd(previous, earned, MaxPlayerExperience)
	previousRank := CompetitiveRankForPoints(previous)
	currentRank := CompetitiveRankForPoints(current)
	info.Point = current
	info.Degree = currentRank.Degree
	return info, ProgressionChange{
		PreviousPoints: previous,
		CurrentPoints:  current,
		EarnedPoints:   earned,
		AppliedPoints:  current - previous,
		PreviousLevel:  previousRank.Degree,
		CurrentLevel:   currentRank.Degree,
		LeveledUp:      currentRank.Degree > previousRank.Degree,
		Capped:         earned > current-previous,
	}
}

// ApplyAdventureExperience applies points using the exact level 0..30 client
// table. Adventure level has no independent wire/database field; it is derived
// from ExtPoint whenever it is displayed or audited.
func ApplyAdventureExperience(info GameInfo, earned uint32) (GameInfo, ProgressionChange) {
	previous := minUint32(info.ExtPoint, MaxAdventureExperience)
	current := boundedExperienceAdd(previous, earned, MaxAdventureExperience)
	previousRank := AdventureRankForPoints(previous)
	currentRank := AdventureRankForPoints(current)
	info.ExtPoint = current
	return info, ProgressionChange{
		PreviousPoints: previous,
		CurrentPoints:  current,
		EarnedPoints:   earned,
		AppliedPoints:  current - previous,
		PreviousLevel:  uint16(previousRank.Level),
		CurrentLevel:   uint16(currentRank.Level),
		LeveledUp:      currentRank.Level > previousRank.Level,
		Capped:         earned > current-previous,
	}
}

func boundedExperienceAdd(value, increment, maximum uint32) uint32 {
	if value >= maximum || increment > maximum-value {
		return maximum
	}
	return value + increment
}

func minUint32(left, right uint32) uint32 {
	if left < right {
		return left
	}
	return right
}
