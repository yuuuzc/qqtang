package game

const (
	patternPointNetworkSize = 11
	patternPointMaxCount    = 15
)

// PatternPoint is the shared per-mode progress record embedded in player
// profiles and login projections.
type PatternPoint struct {
	GameMode     PatternGameMode `json:"game_mode"`
	PatternPoint uint32          `json:"points"`
	PatternLevel uint16          `json:"level"`
	LevelValue   uint32          `json:"level_value"`
}
