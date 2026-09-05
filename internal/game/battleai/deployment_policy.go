package battleai

import (
	"fmt"
	"io"
	"strings"

	"qqtang/internal/game/battleengine"
)

const (
	DeploymentBackendNative      = "native"
	DeploymentBackendONNXRuntime = "onnxruntime"
)

// DeploymentPolicyConfig selects one inference implementation at process
// startup. It never changes the authoritative combat policy or switches
// numerical backends between frames.
type DeploymentPolicyConfig struct {
	Backend            string
	ModelPath          string
	MetadataPath       string
	SharedLibraryPath  string
	IntraOpThreads     int
	InterOpThreads     int
	NativePolicyConfig NativePolicyConfig
}

// LoadedPolicy owns any backend resources needed by a server process.
type LoadedPolicy struct {
	Policy   battleengine.Policy
	Backend  string
	Contract Contract
	Closer   io.Closer
}

// LoadDeploymentPolicy is deliberately strict: a requested ONNX Runtime
// backend must initialize successfully. Operators can explicitly select the
// native backend for recovery, but a packaging error is never hidden by an
// automatic numerical-backend change.
func LoadDeploymentPolicy(config DeploymentPolicyConfig) (LoadedPolicy, error) {
	backend := strings.ToLower(strings.TrimSpace(config.Backend))
	if backend == "" {
		backend = DeploymentBackendNative
	}
	var (
		runner   LogitRunner
		contract Contract
		closer   io.Closer
		err      error
	)
	switch backend {
	case DeploymentBackendNative:
		var native *NativeRunner
		native, err = LoadNativeRunner(config.ModelPath)
		if err == nil {
			runner = native
			contract = native.Contract()
		}
	case DeploymentBackendONNXRuntime:
		runner, contract, closer, err = loadONNXRuntimeDeploymentRunner(config)
	default:
		return LoadedPolicy{}, fmt.Errorf("unsupported AI deployment backend %q", backend)
	}
	if err != nil {
		return LoadedPolicy{}, fmt.Errorf("load %s AI backend: %w", backend, err)
	}
	policy, err := buildActorPolicy(contract, runner, config.NativePolicyConfig)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return LoadedPolicy{}, err
	}
	return LoadedPolicy{
		Policy: policy, Backend: backend, Contract: contract, Closer: closer,
	}, nil
}
