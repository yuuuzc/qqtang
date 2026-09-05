//go:build onnxruntime && cgo

package battleai

import "io"

func loadONNXRuntimeDeploymentRunner(
	config DeploymentPolicyConfig,
) (LogitRunner, Contract, io.Closer, error) {
	runner, err := LoadONNXRuntimeRunner(
		config.ModelPath,
		config.MetadataPath,
		config.SharedLibraryPath,
		ONNXRuntimeConfig{
			IntraOpThreads: config.IntraOpThreads,
			InterOpThreads: config.InterOpThreads,
		},
	)
	if err != nil {
		return nil, Contract{}, nil, err
	}
	return runner, runner.Contract(), runner, nil
}
