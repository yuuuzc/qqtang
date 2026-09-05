package battleai

import (
	"fmt"

	"qqtang/internal/game/battleengine"
)

// NativePolicyConfig describes the complete dependency-free server policy:
// native actor inference proposes visible-state actions and the authoritative
// Go engine may apply a bounded tactical correction to risky decisions.
type NativePolicyConfig struct {
	DangerHorizonMS uint32
	EnableSearch    bool
	Search          battleengine.SearchConfig
}

// LoadNativePolicy constructs a server-ready policy from one QTAI artifact.
// The returned policy owns immutable weights and is safe to share among every
// virtual participant in a match; each participant still receives a separate
// visibility-bounded observation from battleengine.Runtime.
func LoadNativePolicy(modelPath string, config NativePolicyConfig) (battleengine.Policy, error) {
	runner, err := LoadNativeRunner(modelPath)
	if err != nil {
		return nil, err
	}
	return buildActorPolicy(runner.Contract(), runner, config)
}

func buildActorPolicy(
	contract Contract,
	runner LogitRunner,
	config NativePolicyConfig,
) (battleengine.Policy, error) {
	if err := contract.Validate(); err != nil {
		return nil, fmt.Errorf("validate AI contract: %w", err)
	}
	candidates := NeuralCandidates{
		Contract:        contract,
		Runner:          runner,
		DangerHorizonMS: config.DangerHorizonMS,
	}
	if !config.EnableSearch {
		return battleengine.GreedyCandidatePolicy{Candidate: candidates}, nil
	}
	return battleengine.TopKSearchPolicy{Candidate: candidates, Config: config.Search}, nil
}
