//go:build onnxruntime && cgo

package battleai

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	ort "github.com/microsoft/onnxruntime/go/onnxruntime"
)

const (
	minimumONNXRuntimeAPIVersion = 27
	minimumONNXRuntimeMinor      = 29
)

var (
	onnxRuntimeMu   sync.Mutex
	onnxRuntimePath string
)

// ONNXRuntimeConfig controls the CPU execution session. Zero thread counts
// leave ONNX Runtime's platform-specific defaults intact.
type ONNXRuntimeConfig struct {
	IntraOpThreads int
	InterOpThreads int
}

// ONNXRuntimeRunner executes the same decentralized actor contract as the
// dependency-free NativeRunner. The underlying ORT session is safe for
// concurrent inference; input and output tensors remain private to each call.
type ONNXRuntimeRunner struct {
	contract Contract
	session  *ort.Session
}

// LoadONNXRuntimeRunner verifies the model sidecar and initializes the shared
// ONNX Runtime library before constructing a CPU session.
func LoadONNXRuntimeRunner(
	modelPath string,
	metadataPath string,
	sharedLibraryPath string,
	config ONNXRuntimeConfig,
) (*ONNXRuntimeRunner, error) {
	contract, err := LoadContract(modelPath, metadataPath)
	if err != nil {
		return nil, err
	}
	if err := initializeONNXRuntime(sharedLibraryPath); err != nil {
		return nil, err
	}

	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("create ONNX Runtime session options: %w", err)
	}
	defer options.Close()
	if err := options.SetGraphOptimizationLevel(ort.GraphOptimizationLevelAll); err != nil {
		return nil, fmt.Errorf("enable ONNX Runtime graph optimization: %w", err)
	}
	if config.IntraOpThreads > 0 {
		if err := options.SetIntraOpNumThreads(config.IntraOpThreads); err != nil {
			return nil, fmt.Errorf("set ONNX Runtime intra-op threads: %w", err)
		}
	}
	if config.InterOpThreads > 0 {
		if err := options.SetInterOpNumThreads(config.InterOpThreads); err != nil {
			return nil, fmt.Errorf("set ONNX Runtime inter-op threads: %w", err)
		}
	}

	session, err := ort.NewSession(modelPath, options)
	if err != nil {
		return nil, fmt.Errorf("load ONNX actor %s: %w", filepath.Clean(modelPath), err)
	}
	runner := &ONNXRuntimeRunner{contract: contract, session: session}
	if err := runner.validateSessionContract(); err != nil {
		_ = session.Close()
		return nil, err
	}
	return runner, nil
}

func initializeONNXRuntime(sharedLibraryPath string) error {
	if sharedLibraryPath == "" {
		return fmt.Errorf("ONNX Runtime shared library path is empty")
	}
	absolute, err := filepath.Abs(sharedLibraryPath)
	if err != nil {
		return fmt.Errorf("resolve ONNX Runtime shared library path: %w", err)
	}
	absolute = filepath.Clean(absolute)

	onnxRuntimeMu.Lock()
	defer onnxRuntimeMu.Unlock()
	if onnxRuntimePath != "" && !sameONNXRuntimePath(onnxRuntimePath, absolute) {
		return fmt.Errorf("ONNX Runtime is already initialized from %s", onnxRuntimePath)
	}
	ort.SetSharedLibraryPath(absolute)
	if err := ort.Init(); err != nil {
		return fmt.Errorf("initialize ONNX Runtime from %s: %w", absolute, err)
	}
	if apiVersion := ort.APIVersion(); apiVersion < minimumONNXRuntimeAPIVersion {
		return fmt.Errorf(
			"ONNX Runtime API version %d is too old, need at least %d",
			apiVersion,
			minimumONNXRuntimeAPIVersion,
		)
	}
	version, err := ort.GetVersion()
	if err != nil {
		return fmt.Errorf("read ONNX Runtime version: %w", err)
	}
	if !supportedONNXRuntimeVersion(version) {
		return fmt.Errorf("ONNX Runtime version %q is too old, need at least 1.%d", version, minimumONNXRuntimeMinor)
	}
	onnxRuntimePath = absolute
	return nil
}

func supportedONNXRuntimeVersion(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil && (major > 1 || major == 1 && minor >= minimumONNXRuntimeMinor)
}

func sameONNXRuntimePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftAbsolute) == filepath.Clean(rightAbsolute)
}

func (runner *ONNXRuntimeRunner) validateSessionContract() error {
	if runner == nil || runner.session == nil {
		return fmt.Errorf("ONNX Runtime runner is not initialized")
	}
	wantInputs := map[string]struct {
		dtype ort.TensorElementDataType
		shape []int64
	}{
		"spatial": {ort.TensorElementDataTypeFloat32, []int64{-1, int64(runner.contract.Channels), int64(runner.contract.Height), int64(runner.contract.Width)}},
		"scalars": {ort.TensorElementDataTypeFloat32, []int64{-1, int64(runner.contract.Scalars)}},
		"legal":   {ort.TensorElementDataTypeBool, []int64{-1, int64(runner.contract.Actions)}},
	}
	inputs := runner.session.Inputs()
	if len(inputs) != len(wantInputs) {
		return fmt.Errorf("ONNX actor exposes %d inputs, want %d", len(inputs), len(wantInputs))
	}
	for _, input := range inputs {
		want, ok := wantInputs[input.Name]
		if !ok {
			return fmt.Errorf("ONNX actor exposes unexpected input %q", input.Name)
		}
		if input.DataType != want.dtype || !equalONNXShape(input.Shape, want.shape) {
			return fmt.Errorf(
				"ONNX input %q has type/shape %s %v, want %s %v",
				input.Name,
				input.DataType,
				input.Shape,
				want.dtype,
				want.shape,
			)
		}
	}
	outputs := runner.session.Outputs()
	if len(outputs) != 1 || outputs[0].Name != "logits" ||
		outputs[0].DataType != ort.TensorElementDataTypeFloat32 ||
		!equalONNXShape(outputs[0].Shape, []int64{-1, int64(runner.contract.Actions)}) {
		return fmt.Errorf("ONNX actor output contract is not logits[-1,%d] float32", runner.contract.Actions)
	}
	return nil
}

func equalONNXShape(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (runner *ONNXRuntimeRunner) Contract() Contract {
	if runner == nil {
		return Contract{}
	}
	return runner.contract
}

func (runner *ONNXRuntimeRunner) RunActor(
	spatial []float32,
	scalars []float32,
	legal []uint8,
) ([]float32, error) {
	if runner == nil || runner.session == nil {
		return nil, fmt.Errorf("ONNX Runtime runner is not initialized")
	}
	spatialCount := runner.contract.Channels * runner.contract.Height * runner.contract.Width
	if len(spatial) != spatialCount || len(scalars) != runner.contract.Scalars || len(legal) != runner.contract.Actions {
		return nil, fmt.Errorf(
			"ONNX actor input lengths spatial/scalars/legal=%d/%d/%d, want %d/%d/%d",
			len(spatial), len(scalars), len(legal),
			spatialCount, runner.contract.Scalars, runner.contract.Actions,
		)
	}
	legalValues := make([]bool, len(legal))
	for index, value := range legal {
		legalValues[index] = value != 0
	}

	spatialTensor, err := ort.CreateTensor[float32](
		[]int64{1, int64(runner.contract.Channels), int64(runner.contract.Height), int64(runner.contract.Width)},
		spatial,
	)
	if err != nil {
		return nil, fmt.Errorf("create ONNX spatial tensor: %w", err)
	}
	defer spatialTensor.Close()
	scalarTensor, err := ort.CreateTensor[float32]([]int64{1, int64(runner.contract.Scalars)}, scalars)
	if err != nil {
		return nil, fmt.Errorf("create ONNX scalar tensor: %w", err)
	}
	defer scalarTensor.Close()
	legalTensor, err := ort.CreateTensor[bool]([]int64{1, int64(runner.contract.Actions)}, legalValues)
	if err != nil {
		return nil, fmt.Errorf("create ONNX legal tensor: %w", err)
	}
	defer legalTensor.Close()

	outputs, err := runner.session.Run(context.Background(), map[string]*ort.Tensor{
		"spatial": spatialTensor,
		"scalars": scalarTensor,
		"legal":   legalTensor,
	}, []string{"logits"})
	if err != nil {
		return nil, fmt.Errorf("run ONNX actor: %w", err)
	}
	for _, output := range outputs {
		defer output.Close()
	}
	logitTensor := outputs["logits"]
	if logitTensor == nil || !equalONNXShape(logitTensor.Shape(), []int64{1, int64(runner.contract.Actions)}) {
		return nil, fmt.Errorf("ONNX actor returned an invalid logits tensor")
	}
	logits, err := ort.TensorData[float32](logitTensor)
	if err != nil {
		return nil, fmt.Errorf("read ONNX actor logits: %w", err)
	}
	return append([]float32(nil), logits...), nil
}

// Close releases the model session. The process-wide ORT environment remains
// initialized so another runner cannot race a shutdown with concurrent calls.
func (runner *ONNXRuntimeRunner) Close() error {
	if runner == nil || runner.session == nil {
		return nil
	}
	return runner.session.Close()
}
