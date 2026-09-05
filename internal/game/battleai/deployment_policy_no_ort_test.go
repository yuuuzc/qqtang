//go:build !onnxruntime || !cgo

package battleai

import (
	"strings"
	"testing"
)

func TestDeploymentPolicyDoesNotSilentlyFallBackFromONNXRuntime(t *testing.T) {
	_, err := LoadDeploymentPolicy(DeploymentPolicyConfig{
		Backend:   DeploymentBackendONNXRuntime,
		ModelPath: "missing.onnx",
	})
	if err == nil || !strings.Contains(err.Error(), "ONNX Runtime support is not compiled in") {
		t.Fatalf("strict ONNX Runtime deployment error = %v", err)
	}
}
