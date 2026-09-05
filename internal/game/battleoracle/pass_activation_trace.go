package battleoracle

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"qqtang/internal/game/battleengine"
)

type PassActivationCapture struct {
	Header   PassActivationHeader
	Probes   []PassActivationProbe
	Complete bool
}

type PassActivationHeader struct {
	Kind          string          `json:"kind"`
	ObservedUTC   string          `json:"observed_utc"`
	ControllerPID int             `json:"controller_pid"`
	ClientSHA256  string          `json:"client_sha256"`
	Native        json.RawMessage `json:"native"`
}

type PassActivationProbe struct {
	Kind          string `json:"kind"`
	Collector     string `json:"collector"`
	Index         int    `json:"index"`
	Name          string `json:"name"`
	ElapsedMS     uint32 `json:"elapsed_ms"`
	FreshMS       uint32 `json:"fresh_ms"`
	Clock         uint32 `json:"clock"`
	BaselineState string `json:"baseline_state"`
	InjectedState string `json:"injected_state"`
	ResultState   string `json:"result_state"`
}

type PassActivationCaseDefinition struct {
	Name      string `json:"name"`
	ElapsedMS uint32 `json:"elapsed_ms"`
	FreshMS   uint32 `json:"fresh_ms"`
}

type passActivationNativeSource struct {
	Kind               string                         `json:"kind"`
	Collector          string                         `json:"collector"`
	PID                int                            `json:"pid"`
	ClientPath         string                         `json:"client_path"`
	ClientBase         string                         `json:"client_base"`
	ClientSize         uint64                         `json:"client_size"`
	PassActivateRVA    uint32                         `json:"pass_activate_rva"`
	PassActivateABI    string                         `json:"pass_activate_abi"`
	SchedulerRVA       uint32                         `json:"scheduler_rva"`
	SchedulerABI       string                         `json:"scheduler_abi"`
	CurrentActorOffset uint32                         `json:"current_actor_offset"`
	PassStateOffset    uint32                         `json:"pass_state_offset"`
	GlobalRootRVA      uint32                         `json:"global_root_rva"`
	ScenePointerOffset uint32                         `json:"scene_pointer_offset"`
	SceneClockOffset   uint32                         `json:"scene_clock_offset"`
	PassStateSize      uint32                         `json:"pass_state_size"`
	Cases              []PassActivationCaseDefinition `json:"cases"`
}

type PassActivationDifference struct {
	ProbeIndex int    `json:"probe_index"`
	Case       string `json:"case"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
}

type PassActivationReport struct {
	ProbeCount  int                        `json:"probe_count"`
	Differences []PassActivationDifference `json:"differences"`
}

func (report PassActivationReport) Matches() bool { return len(report.Differences) == 0 }

var expectedPassActivationCases = []PassActivationCaseDefinition{
	{Name: "elapsed_499", ElapsedMS: 499, FreshMS: 0},
	{Name: "elapsed_500", ElapsedMS: 500, FreshMS: 0},
	{Name: "elapsed_501", ElapsedMS: 501, FreshMS: 0},
	{Name: "elapsed_599", ElapsedMS: 599, FreshMS: 0},
	{Name: "elapsed_600", ElapsedMS: 600, FreshMS: 0},
	{Name: "fresh_99", ElapsedMS: 501, FreshMS: 99},
	{Name: "fresh_100", ElapsedMS: 501, FreshMS: 100},
}

func DecodePassActivationCapture(reader io.Reader) (PassActivationCapture, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var capture PassActivationCapture
	headerSeen := false
	completeTotal := -1
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Bytes()
		var envelope staticTraceEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return PassActivationCapture{}, fmt.Errorf("decode pass activation trace line %d: %w", lineNumber, err)
		}
		switch envelope.Kind {
		case "collection_header":
			if headerSeen {
				return PassActivationCapture{}, fmt.Errorf("pass activation trace repeats header at line %d", lineNumber)
			}
			if err := decodeStrictJSON(line, &capture.Header); err != nil {
				return PassActivationCapture{}, fmt.Errorf("decode pass activation header line %d: %w", lineNumber, err)
			}
			headerSeen = true
		case "pass_activation_probe":
			if !headerSeen {
				return PassActivationCapture{}, fmt.Errorf("pass activation probe precedes header at line %d", lineNumber)
			}
			var probe PassActivationProbe
			if err := decodeStrictJSON(line, &probe); err != nil {
				return PassActivationCapture{}, fmt.Errorf("decode pass activation probe line %d: %w", lineNumber, err)
			}
			capture.Probes = append(capture.Probes, probe)
		case "micro_complete":
			var complete struct {
				Kind      string `json:"kind"`
				Collector string `json:"collector"`
				Total     int    `json:"total"`
			}
			if err := decodeStrictJSON(line, &complete); err != nil {
				return PassActivationCapture{}, fmt.Errorf("decode pass activation completion line %d: %w", lineNumber, err)
			}
			if complete.Collector != "qqt-rule1-pass-activation-micro-oracle/v1" {
				return PassActivationCapture{}, fmt.Errorf("pass activation completion has incompatible collector %q", complete.Collector)
			}
			capture.Complete = true
			completeTotal = complete.Total
		case "collection_footer":
			// Completion is authoritative; a timeout footer is never sufficient.
		case "micro_error":
			return PassActivationCapture{}, fmt.Errorf("pass activation native collector reported an error at line %d", lineNumber)
		default:
			return PassActivationCapture{}, fmt.Errorf("pass activation trace line %d has unknown kind %q", lineNumber, envelope.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return PassActivationCapture{}, fmt.Errorf("read pass activation trace: %w", err)
	}
	if !headerSeen || !capture.Complete || completeTotal != len(expectedPassActivationCases) || len(capture.Probes) != completeTotal {
		return PassActivationCapture{}, fmt.Errorf("pass activation trace is incomplete: probes=%d total=%d complete=%t", len(capture.Probes), completeTotal, capture.Complete)
	}
	if len(capture.Header.ClientSHA256) != 64 {
		return PassActivationCapture{}, fmt.Errorf("pass activation client SHA-256 has length %d", len(capture.Header.ClientSHA256))
	}
	if _, err := hex.DecodeString(capture.Header.ClientSHA256); err != nil {
		return PassActivationCapture{}, fmt.Errorf("pass activation client SHA-256 is invalid: %w", err)
	}
	var native passActivationNativeSource
	if err := decodeStrictJSON(capture.Header.Native, &native); err != nil {
		return PassActivationCapture{}, fmt.Errorf("pass activation native provenance: %w", err)
	}
	if native.Collector != "qqt-rule1-pass-activation-micro-oracle/v1" || native.PassActivateRVA != 0x1d33ab ||
		native.PassActivateABI != "fastcall_ecx_state" || native.SchedulerRVA != 0x1cfc10 ||
		native.SchedulerABI != "stdcall_natural_game_thread" || native.CurrentActorOffset != 0x3828 ||
		native.PassStateOffset != 0x350 || native.GlobalRootRVA != 0x406a78 ||
		native.ScenePointerOffset != 0x7d4 || native.SceneClockOffset != 0x4b48 || native.PassStateSize != 0x1c ||
		!samePassActivationCases(native.Cases, expectedPassActivationCases) {
		return PassActivationCapture{}, fmt.Errorf("pass activation native provenance is incompatible: %+v", native)
	}
	for index, probe := range capture.Probes {
		expected := expectedPassActivationCases[index]
		if probe.Collector != native.Collector || probe.Index != index || probe.Name != expected.Name ||
			probe.ElapsedMS != expected.ElapsedMS || probe.FreshMS != expected.FreshMS {
			return PassActivationCapture{}, fmt.Errorf("pass activation probe %d does not match the fixed suite", index)
		}
		for label, encoded := range map[string]string{"baseline": probe.BaselineState, "injected": probe.InjectedState, "result": probe.ResultState} {
			bytes, err := hex.DecodeString(encoded)
			if err != nil || len(bytes) != 0x1c {
				return PassActivationCapture{}, fmt.Errorf("pass activation probe %d has invalid %s state", index, label)
			}
		}
		injected, err := decodeDynamicPassState(probe.InjectedState)
		if err != nil {
			return PassActivationCapture{}, fmt.Errorf("pass activation probe %d injected state: %w", index, err)
		}
		if !injected.CollisionValid || injected.CollisionCell != (battleengine.Cell{Row: 1, Col: 1}) || injected.Active ||
			probe.Clock-injected.CollisionStartedAt != probe.ElapsedMS || probe.Clock-injected.CollisionLastAt != probe.FreshMS ||
			injected.StartedAt != 0 || injected.DurationMS != 0 {
			return PassActivationCapture{}, fmt.Errorf("pass activation probe %d has inconsistent injected state %+v", index, injected)
		}
	}
	return capture, nil
}

func ComparePassActivationCapture(capture PassActivationCapture) (PassActivationReport, error) {
	report := PassActivationReport{ProbeCount: len(capture.Probes)}
	for _, probe := range capture.Probes {
		injected, _ := decodeDynamicPassState(probe.InjectedState)
		native, _ := decodeDynamicPassState(probe.ResultState)
		// Duration is attribute-dependent inside FUN_005d33ab. A fixed nonzero
		// speed exercises the same production predicate, while this comparator
		// verifies the source-independent fields and requires a positive native
		// duration only when activation succeeds.
		actual := battleengine.ResolveNativePassActivateProbe(injected, probe.Clock, 40)
		match := actual.CollisionValid == native.CollisionValid && actual.CollisionCell == native.CollisionCell &&
			actual.CollisionStartedAt == native.CollisionStartedAt && actual.CollisionLastAt == native.CollisionLastAt &&
			actual.Active == native.Active && actual.StartedAt == native.StartedAt
		if native.Active {
			match = match && native.DurationMS > 0
		} else {
			match = match && native.DurationMS == 0
		}
		if !match {
			report.Differences = append(report.Differences, PassActivationDifference{
				ProbeIndex: probe.Index, Case: probe.Name,
				Expected: fmt.Sprintf("native valid=%t cell=%+v collision=%d/%d active=%t started=%d duration=%d", native.CollisionValid, native.CollisionCell, native.CollisionStartedAt, native.CollisionLastAt, native.Active, native.StartedAt, native.DurationMS),
				Actual:   fmt.Sprintf("go valid=%t cell=%+v collision=%d/%d active=%t started=%d", actual.CollisionValid, actual.CollisionCell, actual.CollisionStartedAt, actual.CollisionLastAt, actual.Active, actual.StartedAt),
			})
		}
	}
	return report, nil
}

func samePassActivationCases(left, right []PassActivationCaseDefinition) bool {
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
