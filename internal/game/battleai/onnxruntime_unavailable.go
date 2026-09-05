//go:build !onnxruntime || !cgo

package battleai

import (
	"fmt"
	"io"
)

func loadONNXRuntimeDeploymentRunner(
	_ DeploymentPolicyConfig,
) (LogitRunner, Contract, io.Closer, error) {
	return nil, Contract{}, nil, fmt.Errorf(
		"ONNX Runtime support is not compiled in; rebuild with CGO_ENABLED=1 and -tags onnxruntime",
	)
}
