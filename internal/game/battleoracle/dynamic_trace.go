package battleoracle

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/mapdata"
)

type DynamicMovementCapture struct {
	Header   DynamicMovementHeader
	Calls    []DynamicMovementCall
	Complete DynamicMovementComplete
}

type DynamicMovementHeader struct {
	Kind          string                      `json:"kind"`
	Collector     string                      `json:"collector"`
	ObservedUTC   string                      `json:"observed_utc"`
	ControllerPID int                         `json:"controller_pid"`
	MapID         uint32                      `json:"map_id"`
	MapHash       string                      `json:"map_hash"`
	ClientSHA256  string                      `json:"client_sha256"`
	Native        DynamicMovementNativeSource `json:"native"`
}

type DynamicMovementNativeSource struct {
	Kind                 string `json:"kind"`
	Collector            string `json:"collector"`
	PID                  int    `json:"pid"`
	Architecture         string `json:"architecture"`
	ClientPath           string `json:"client_path"`
	ClientBase           string `json:"client_base"`
	ClientSize           uint64 `json:"client_size"`
	MovementResolverRVA  uint32 `json:"movement_resolver_rva"`
	MovementResolverABI  string `json:"movement_resolver_abi"`
	DeltaMSType          string `json:"delta_ms_type"`
	MovementResolver     string `json:"movement_resolver"`
	CellResolverRVA      uint32 `json:"cell_resolver_rva"`
	CellClassifierRVA    uint32 `json:"cell_classifier_rva"`
	DynamicBoundaryRVA   uint32 `json:"dynamic_boundary_rva"`
	DynamicCellLookupRVA uint32 `json:"dynamic_cell_lookup_rva"`
	PassTouchRVA         uint32 `json:"pass_touch_rva"`
	PassActivateRVA      uint32 `json:"pass_activate_rva"`
	CurrentActorOffset   uint32 `json:"current_actor_offset"`
	PassStateOffset      uint32 `json:"pass_state_offset"`
	PassStateSize        uint32 `json:"pass_state_size"`
}

type DynamicMovementCall struct {
	Kind                 string                    `json:"kind"`
	ObservedUTC          string                    `json:"observed_utc"`
	Sequence             int                       `json:"sequence"`
	TimestampMS          uint64                    `json:"timestamp_ms"`
	ThreadID             uint32                    `json:"thread_id"`
	Caller               string                    `json:"caller"`
	Manager              string                    `json:"manager"`
	X                    float64                   `json:"x"`
	Y                    float64                   `json:"y"`
	ActorContext         string                    `json:"actor_context"`
	Direction            uint8                     `json:"direction"`
	VelocityX            float64                   `json:"velocity_x"`
	VelocityY            float64                   `json:"velocity_y"`
	DeltaMS              int32                     `json:"delta_ms"`
	CornerTolerance      uint16                    `json:"corner_tolerance"`
	PassStateBefore      *DynamicActorPassSnapshot `json:"pass_state_before"`
	DynamicCellLookups   []DynamicCellLookup       `json:"dynamic_cell_lookups"`
	CellClassifierCalls  []DynamicCellClassifier   `json:"cell_classifier_calls"`
	DynamicBoundaryCalls []DynamicBoundaryCall     `json:"dynamic_boundary_calls"`
	CellResolverCalls    []DynamicCellResolver     `json:"cell_resolver_calls"`
	PassTouchCalls       []DynamicPassTouch        `json:"pass_touch_calls"`
	PassActivateCalls    []DynamicPassActivate     `json:"pass_activate_calls"`
	OutX                 *float64                  `json:"out_x"`
	OutY                 *float64                  `json:"out_y"`
	BlockedX             *uint32                   `json:"blocked_x"`
	BlockedY             *uint32                   `json:"blocked_y"`
	ReturnValue          uint32                    `json:"return_value"`
	PassStateAfter       *DynamicActorPassSnapshot `json:"pass_state_after"`
}

type DynamicActorPassSnapshot struct {
	Actor string `json:"actor"`
	Bytes string `json:"bytes"`
}

type DynamicCellLookup struct {
	Caller      string   `json:"caller"`
	Container   string   `json:"container"`
	Row         int32    `json:"row"`
	Col         int32    `json:"col"`
	Result      string   `json:"result"`
	ObjectCount *int     `json:"object_count"`
	ObjectTypes []*uint8 `json:"object_types"`
	Sequence    int      `json:"sequence"`
}

type DynamicCellClassifier struct {
	Caller        string  `json:"caller"`
	Row           int32   `json:"row"`
	Col           int32   `json:"col"`
	Direction     uint8   `json:"direction"`
	ActorContext  string  `json:"actor_context"`
	ReturnValue   uint8   `json:"return_value"`
	CollisionKind *uint32 `json:"collision_kind"`
	Sequence      int     `json:"sequence"`
}

type DynamicBoundaryCall struct {
	Caller        string  `json:"caller"`
	CornerIndex   int32   `json:"corner_index"`
	Scratch1      int32   `json:"x1"`
	Scratch2      int32   `json:"y1"`
	Scratch3      int32   `json:"x2"`
	Scratch4      int32   `json:"y2"`
	Row           int32   `json:"row"`
	Col           int32   `json:"col"`
	Direction     uint8   `json:"direction"`
	IgnoreDynamic uint8   `json:"ignore_dynamic"`
	ReturnValue   uint8   `json:"return_value"`
	CollisionKind *uint32 `json:"collision_kind"`
	Sequence      int     `json:"sequence"`
}

type DynamicCellResolver struct {
	Caller          string                    `json:"caller"`
	Manager         string                    `json:"manager"`
	X               uint16                    `json:"x"`
	XStackRaw       uint32                    `json:"x_stack_raw"`
	Y               int32                     `json:"y"`
	Direction       uint8                     `json:"direction"`
	ActorContext    string                    `json:"actor_context"`
	TrackPassState  uint8                     `json:"track_pass_state"`
	PassStateBefore *DynamicActorPassSnapshot `json:"pass_state_before"`
	ReturnValue     uint8                     `json:"return_value"`
	CollisionKind   *uint32                   `json:"collision_kind"`
	PassStateAfter  *DynamicActorPassSnapshot `json:"pass_state_after"`
	Sequence        int                       `json:"sequence"`
}

type DynamicPassTouch struct {
	Caller      string `json:"caller"`
	State       string `json:"state"`
	Row         int32  `json:"row"`
	Col         int32  `json:"col"`
	StateBefore string `json:"state_before"`
	StateAfter  string `json:"state_after"`
	Sequence    int    `json:"sequence"`
}

type DynamicPassActivate struct {
	Caller      string `json:"caller"`
	State       string `json:"state"`
	StateBefore string `json:"state_before"`
	StateAfter  string `json:"state_after"`
	Sequence    int    `json:"sequence"`
}

type DynamicMovementComplete struct {
	Kind          string `json:"kind"`
	Collector     string `json:"collector"`
	ObservedUTC   string `json:"observed_utc"`
	MovementCalls int    `json:"movement_calls"`
}

type DynamicMovementDifference struct {
	MovementSequence int    `json:"movement_sequence"`
	NestedSequence   int    `json:"nested_sequence"`
	Family           string `json:"family"`
	Expected         string `json:"expected"`
	Actual           string `json:"actual"`
}

type DynamicMovementReport struct {
	MapID                   uint32                      `json:"map_id"`
	MovementCalls           int                         `json:"movement_calls"`
	CellLookups             int                         `json:"cell_lookups"`
	NonEmptyCellLookups     int                         `json:"nonempty_cell_lookups"`
	TypeOneObjectReferences int                         `json:"type_one_object_references"`
	BoundaryCalls           int                         `json:"boundary_calls"`
	PassTouchCalls          int                         `json:"pass_touch_calls"`
	PassActivateCalls       int                         `json:"pass_activate_calls"`
	Differences             []DynamicMovementDifference `json:"differences"`
}

func (report DynamicMovementReport) Matches() bool { return len(report.Differences) == 0 }

func DecodeDynamicMovementCapture(reader io.Reader) (DynamicMovementCapture, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var capture DynamicMovementCapture
	headerSeen, completeSeen := false, false
	lastNestedSequence := 0
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Bytes()
		var envelope staticTraceEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return DynamicMovementCapture{}, fmt.Errorf("decode dynamic movement trace line %d: %w", lineNumber, err)
		}
		if completeSeen {
			return DynamicMovementCapture{}, fmt.Errorf("dynamic movement trace has data after completion at line %d", lineNumber)
		}
		switch envelope.Kind {
		case "collection_header":
			if headerSeen {
				return DynamicMovementCapture{}, fmt.Errorf("dynamic movement trace repeats collection header at line %d", lineNumber)
			}
			if err := decodeStrictJSON(line, &capture.Header); err != nil {
				return DynamicMovementCapture{}, fmt.Errorf("decode dynamic movement header line %d: %w", lineNumber, err)
			}
			headerSeen = true
		case "movement_call":
			if !headerSeen {
				return DynamicMovementCapture{}, fmt.Errorf("dynamic movement call precedes header at line %d", lineNumber)
			}
			var call DynamicMovementCall
			if err := decodeStrictJSON(line, &call); err != nil {
				return DynamicMovementCapture{}, fmt.Errorf("decode dynamic movement call line %d: %w", lineNumber, err)
			}
			if call.Sequence != len(capture.Calls)+1 {
				return DynamicMovementCapture{}, fmt.Errorf("dynamic movement call order %d has native sequence %d", len(capture.Calls)+1, call.Sequence)
			}
			sequences := dynamicNestedSequences(call)
			sort.Ints(sequences)
			for _, sequence := range sequences {
				if sequence != lastNestedSequence+1 {
					return DynamicMovementCapture{}, fmt.Errorf("dynamic nested sequence after %d is %d at movement %d", lastNestedSequence, sequence, call.Sequence)
				}
				lastNestedSequence = sequence
			}
			if err := validateDynamicCall(call); err != nil {
				return DynamicMovementCapture{}, fmt.Errorf("dynamic movement call %d: %w", call.Sequence, err)
			}
			capture.Calls = append(capture.Calls, call)
		case "collection_complete":
			if !headerSeen {
				return DynamicMovementCapture{}, fmt.Errorf("dynamic movement completion precedes header at line %d", lineNumber)
			}
			if err := decodeStrictJSON(line, &capture.Complete); err != nil {
				return DynamicMovementCapture{}, fmt.Errorf("decode dynamic movement completion line %d: %w", lineNumber, err)
			}
			completeSeen = true
		default:
			return DynamicMovementCapture{}, fmt.Errorf("dynamic movement trace line %d has unknown kind %q", lineNumber, envelope.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return DynamicMovementCapture{}, fmt.Errorf("read dynamic movement trace: %w", err)
	}
	if !headerSeen || !completeSeen || len(capture.Calls) == 0 {
		return DynamicMovementCapture{}, fmt.Errorf("dynamic movement trace is incomplete: header=%t calls=%d complete=%t", headerSeen, len(capture.Calls), completeSeen)
	}
	if capture.Complete.MovementCalls != len(capture.Calls) {
		return DynamicMovementCapture{}, fmt.Errorf("dynamic movement completion says %d calls, decoded %d", capture.Complete.MovementCalls, len(capture.Calls))
	}
	if capture.Complete.Collector != "qqt-rule1-movement-oracle-host/v2" {
		return DynamicMovementCapture{}, fmt.Errorf("dynamic movement completion provenance is incompatible")
	}
	if err := validateDynamicHeader(capture.Header); err != nil {
		return DynamicMovementCapture{}, err
	}
	return capture, nil
}

func validateDynamicHeader(header DynamicMovementHeader) error {
	if header.Collector != "qqt-rule1-movement-oracle-host/v2" || header.MapID == 0 {
		return fmt.Errorf("dynamic movement host provenance is incompatible")
	}
	for label, hash := range map[string]string{"client": header.ClientSHA256, "map": header.MapHash} {
		if len(hash) != 64 {
			return fmt.Errorf("dynamic movement %s SHA-256 has length %d", label, len(hash))
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fmt.Errorf("dynamic movement %s SHA-256 is invalid: %w", label, err)
		}
	}
	native := header.Native
	if native.Kind != "ready" || native.Collector != "qqt-rule1-movement-oracle/v4" || native.Architecture != "ia32" ||
		native.MovementResolverRVA != 0x1cfc10 || native.MovementResolverABI != "stdcall" || native.DeltaMSType != "int32" ||
		native.CellResolverRVA != 0x1b7999 || native.CellClassifierRVA != 0x1b7dc7 || native.DynamicBoundaryRVA != 0x1b7f5c ||
		native.DynamicCellLookupRVA != 0x1d3bd3 || native.PassTouchRVA != 0x1d335c || native.PassActivateRVA != 0x1d33ab ||
		native.CurrentActorOffset != 0x3828 || native.PassStateOffset != 0x350 || native.PassStateSize != 0x1c {
		return fmt.Errorf("dynamic movement native provenance is incompatible: %+v", native)
	}
	return nil
}

func validateDynamicCall(call DynamicMovementCall) error {
	if call.Direction > 3 || call.DeltaMS <= 0 || call.CornerTolerance != battleengine.NativeCornerSlideTolerancePixels {
		return fmt.Errorf("invalid direction/delta/corner tuple %d/%d/%d", call.Direction, call.DeltaMS, call.CornerTolerance)
	}
	if call.ReturnValue > 1 || call.OutX == nil || call.OutY == nil || call.BlockedX == nil || call.BlockedY == nil {
		return fmt.Errorf("movement result is incomplete")
	}
	for _, lookup := range call.DynamicCellLookups {
		if lookup.ObjectCount == nil || *lookup.ObjectCount < 0 {
			return fmt.Errorf("lookup %d has invalid object count", lookup.Sequence)
		}
		if *lookup.ObjectCount <= 64 && len(lookup.ObjectTypes) != *lookup.ObjectCount {
			return fmt.Errorf("lookup %d count=%d types=%d", lookup.Sequence, *lookup.ObjectCount, len(lookup.ObjectTypes))
		}
	}
	for _, snapshot := range []*DynamicActorPassSnapshot{call.PassStateBefore, call.PassStateAfter} {
		if snapshot != nil {
			if _, err := decodeDynamicPassState(snapshot.Bytes); err != nil {
				return err
			}
		}
	}
	for _, touch := range call.PassTouchCalls {
		if _, err := decodeDynamicPassState(touch.StateBefore); err != nil {
			return fmt.Errorf("pass touch %d before: %w", touch.Sequence, err)
		}
		if _, err := decodeDynamicPassState(touch.StateAfter); err != nil {
			return fmt.Errorf("pass touch %d after: %w", touch.Sequence, err)
		}
	}
	for _, activation := range call.PassActivateCalls {
		if _, err := decodeDynamicPassState(activation.StateBefore); err != nil {
			return fmt.Errorf("pass activate %d before: %w", activation.Sequence, err)
		}
		if _, err := decodeDynamicPassState(activation.StateAfter); err != nil {
			return fmt.Errorf("pass activate %d after: %w", activation.Sequence, err)
		}
	}
	return nil
}

func dynamicNestedSequences(call DynamicMovementCall) []int {
	sequences := make([]int, 0, len(call.DynamicCellLookups)+len(call.CellClassifierCalls)+len(call.DynamicBoundaryCalls)+len(call.CellResolverCalls)+len(call.PassTouchCalls)+len(call.PassActivateCalls))
	for _, value := range call.DynamicCellLookups {
		sequences = append(sequences, value.Sequence)
	}
	for _, value := range call.CellClassifierCalls {
		sequences = append(sequences, value.Sequence)
	}
	for _, value := range call.DynamicBoundaryCalls {
		sequences = append(sequences, value.Sequence)
	}
	for _, value := range call.CellResolverCalls {
		sequences = append(sequences, value.Sequence)
	}
	for _, value := range call.PassTouchCalls {
		sequences = append(sequences, value.Sequence)
	}
	for _, value := range call.PassActivateCalls {
		sequences = append(sequences, value.Sequence)
	}
	return sequences
}

func CompareDynamicMovementCapture(capture DynamicMovementCapture, entry mapdata.CompetitiveMap, mapFileSHA256 string) (DynamicMovementReport, error) {
	if capture.Header.MapID != entry.ID {
		return DynamicMovementReport{}, fmt.Errorf("dynamic movement trace map %d does not match catalog map %d", capture.Header.MapID, entry.ID)
	}
	if !strings.EqualFold(capture.Header.MapHash, mapFileSHA256) {
		return DynamicMovementReport{}, fmt.Errorf("dynamic movement trace map SHA-256 %s does not match installed file %s", capture.Header.MapHash, mapFileSHA256)
	}
	report := DynamicMovementReport{MapID: entry.ID, MovementCalls: len(capture.Calls)}
	for _, call := range capture.Calls {
		lookupBySequence := make(map[int]DynamicCellLookup, len(call.DynamicCellLookups))
		for _, lookup := range call.DynamicCellLookups {
			report.CellLookups++
			lookupBySequence[lookup.Sequence] = lookup
			if lookup.ObjectCount != nil && *lookup.ObjectCount > 0 {
				report.NonEmptyCellLookups++
			}
			for _, objectType := range lookup.ObjectTypes {
				if objectType != nil && *objectType == 1 {
					report.TypeOneObjectReferences++
				}
			}
		}
		for _, boundary := range call.DynamicBoundaryCalls {
			report.BoundaryCalls++
			lookup, found := lookupBySequence[boundary.Sequence-1]
			if !found || lookup.Row != boundary.Row || lookup.Col != boundary.Col {
				report.Differences = append(report.Differences, DynamicMovementDifference{MovementSequence: call.Sequence, NestedSequence: boundary.Sequence, Family: "dynamic-boundary-correlation", Expected: "immediately preceding lookup of the same cell", Actual: "lookup missing or cell differs"})
				continue
			}
			if lookup.ObjectCount != nil && *lookup.ObjectCount == 0 && (boundary.ReturnValue != 1 || boundary.CollisionKind == nil || *boundary.CollisionKind != 0) {
				report.Differences = append(report.Differences, DynamicMovementDifference{MovementSequence: call.Sequence, NestedSequence: boundary.Sequence, Family: "dynamic-boundary-empty", Expected: "return=1 collision=0", Actual: fmt.Sprintf("return=%d collision=%v", boundary.ReturnValue, boundary.CollisionKind)})
			}
		}
		for _, touch := range call.PassTouchCalls {
			report.PassTouchCalls++
			before, _ := decodeDynamicPassState(touch.StateBefore)
			after, _ := decodeDynamicPassState(touch.StateAfter)
			if touch.Row < math.MinInt16 || touch.Row > math.MaxInt16 || touch.Col < math.MinInt16 || touch.Col > math.MaxInt16 {
				return DynamicMovementReport{}, fmt.Errorf("pass touch %d cell %d,%d is outside engine coordinates", touch.Sequence, touch.Row, touch.Col)
			}
			actual := battleengine.ResolveNativePassTouchProbe(before, after.CollisionLastAt, battleengine.Cell{Row: int16(touch.Row), Col: int16(touch.Col)})
			if actual != after {
				report.Differences = append(report.Differences, DynamicMovementDifference{MovementSequence: call.Sequence, NestedSequence: touch.Sequence, Family: "pass-touch", Expected: fmt.Sprintf("native %+v", after), Actual: fmt.Sprintf("go %+v", actual)})
			}
		}
		report.PassActivateCalls += len(call.PassActivateCalls)
	}
	return report, nil
}

func decodeDynamicPassState(encoded string) (battleengine.NativePassProbeState, error) {
	bytes, err := hex.DecodeString(encoded)
	if err != nil || len(bytes) != 0x1c {
		return battleengine.NativePassProbeState{}, fmt.Errorf("invalid 0x1c-byte pass-state snapshot")
	}
	col := int32(binary.LittleEndian.Uint32(bytes[4:8]))
	row := int32(binary.LittleEndian.Uint32(bytes[8:12]))
	valid := col != -1 || row != -1
	if (col == -1) != (row == -1) || (valid && (row < math.MinInt16 || row > math.MaxInt16 || col < math.MinInt16 || col > math.MaxInt16)) {
		return battleengine.NativePassProbeState{}, fmt.Errorf("invalid pass collision cell %d,%d", row, col)
	}
	state := battleengine.NativePassProbeState{
		CollisionValid:     valid,
		CollisionStartedAt: binary.LittleEndian.Uint32(bytes[12:16]),
		CollisionLastAt:    binary.LittleEndian.Uint32(bytes[16:20]),
		Active:             bytes[0] != 0,
		StartedAt:          binary.LittleEndian.Uint32(bytes[20:24]),
		DurationMS:         binary.LittleEndian.Uint32(bytes[24:28]),
	}
	if valid {
		state.CollisionCell = battleengine.Cell{Row: int16(row), Col: int16(col)}
	}
	return state, nil
}
