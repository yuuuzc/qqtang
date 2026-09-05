package battleoracle

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/mapdata"
)

type StaticMovementCapture struct {
	Header   StaticMovementHeader
	Probes   []StaticMovementProbe
	Complete bool
}

type StaticMovementHeader struct {
	Kind          string          `json:"kind"`
	ObservedUTC   string          `json:"observed_utc"`
	ControllerPID int             `json:"controller_pid"`
	MapID         uint32          `json:"map_id"`
	MapHash       string          `json:"map_hash"`
	GridWidth     uint16          `json:"grid_width"`
	GridHeight    uint16          `json:"grid_height"`
	ClientSHA256  string          `json:"client_sha256"`
	Native        json.RawMessage `json:"native"`
}

type StaticMovementProbe struct {
	Kind            string  `json:"kind"`
	Index           int     `json:"index"`
	Family          string  `json:"family"`
	Row             int     `json:"row,omitempty"`
	Col             int     `json:"col,omitempty"`
	BoundaryRow     int     `json:"boundary_row,omitempty"`
	BoundaryCol     int     `json:"boundary_col,omitempty"`
	Offset          int     `json:"offset,omitempty"`
	X               float64 `json:"x"`
	Y               float64 `json:"y"`
	Direction       uint8   `json:"direction"`
	VelocityX       float64 `json:"velocity_x"`
	VelocityY       float64 `json:"velocity_y"`
	DeltaMS         uint32  `json:"delta_ms"`
	CornerTolerance uint16  `json:"corner_tolerance"`
	OutX            float64 `json:"out_x"`
	OutY            float64 `json:"out_y"`
	BlockedX        uint32  `json:"blocked_x"`
	BlockedY        uint32  `json:"blocked_y"`
	ReturnValue     uint32  `json:"return_value"`
	PassStateAfter  string  `json:"pass_state_after"`
}

type StaticMovementDifference struct {
	ProbeIndex int    `json:"probe_index"`
	Family     string `json:"family"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
}

type StaticMovementReport struct {
	MapID       uint32                     `json:"map_id"`
	ProbeCount  int                        `json:"probe_count"`
	Differences []StaticMovementDifference `json:"differences"`
}

func (report StaticMovementReport) Matches() bool { return len(report.Differences) == 0 }

type staticTraceEnvelope struct {
	Kind string `json:"kind"`
}

type staticMovementNativeSource struct {
	Kind                string `json:"kind"`
	Collector           string `json:"collector"`
	PID                 int    `json:"pid"`
	ClientPath          string `json:"client_path"`
	ClientBase          string `json:"client_base"`
	ClientSize          uint64 `json:"client_size"`
	MovementResolverRVA uint32 `json:"movement_resolver_rva"`
	MovementResolverABI string `json:"movement_resolver_abi"`
	DeltaMSType         string `json:"delta_ms_type"`
	CurrentActorOffset  uint32 `json:"current_actor_offset"`
	PassStateOffset     uint32 `json:"pass_state_offset"`
	PassStateSize       uint32 `json:"pass_state_size"`
	GridWidthOffset     uint32 `json:"grid_width_offset"`
	GridHeightOffset    uint32 `json:"grid_height_offset"`
}

func DecodeStaticMovementCapture(reader io.Reader) (StaticMovementCapture, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var capture StaticMovementCapture
	headerSeen := false
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Bytes()
		var envelope staticTraceEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return StaticMovementCapture{}, fmt.Errorf("decode static movement trace line %d: %w", lineNumber, err)
		}
		switch envelope.Kind {
		case "collection_header":
			if headerSeen {
				return StaticMovementCapture{}, fmt.Errorf("static movement trace repeats collection header at line %d", lineNumber)
			}
			if err := decodeStrictJSON(line, &capture.Header); err != nil {
				return StaticMovementCapture{}, fmt.Errorf("decode static movement header line %d: %w", lineNumber, err)
			}
			headerSeen = true
		case "static_movement_probe":
			if !headerSeen {
				return StaticMovementCapture{}, fmt.Errorf("static movement probe precedes header at line %d", lineNumber)
			}
			var probe StaticMovementProbe
			if err := decodeStrictJSON(line, &probe); err != nil {
				return StaticMovementCapture{}, fmt.Errorf("decode static movement probe line %d: %w", lineNumber, err)
			}
			capture.Probes = append(capture.Probes, probe)
		case "micro_complete":
			capture.Complete = true
		case "collection_footer":
			// The explicit micro_complete record is authoritative. A timeout also
			// writes a footer, but must never be mistaken for a complete suite.
		case "micro_error":
			return StaticMovementCapture{}, fmt.Errorf("static movement native collector reported an error at line %d", lineNumber)
		default:
			return StaticMovementCapture{}, fmt.Errorf("static movement trace line %d has unknown kind %q", lineNumber, envelope.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return StaticMovementCapture{}, fmt.Errorf("read static movement trace: %w", err)
	}
	if !headerSeen || capture.Header.MapID == 0 || capture.Header.GridWidth == 0 || capture.Header.GridHeight == 0 {
		return StaticMovementCapture{}, fmt.Errorf("static movement trace has no complete header")
	}
	if len(capture.Header.ClientSHA256) != 64 {
		return StaticMovementCapture{}, fmt.Errorf("static movement trace client SHA-256 has length %d", len(capture.Header.ClientSHA256))
	}
	if _, err := hex.DecodeString(capture.Header.ClientSHA256); err != nil {
		return StaticMovementCapture{}, fmt.Errorf("static movement trace client SHA-256 is invalid: %w", err)
	}
	var native staticMovementNativeSource
	if err := decodeStrictJSON(capture.Header.Native, &native); err != nil {
		return StaticMovementCapture{}, fmt.Errorf("static movement native provenance: %w", err)
	}
	if native.Collector != "qqt-rule1-static-micro-oracle/v2" || native.MovementResolverRVA != 0x1cfc10 ||
		native.MovementResolverABI != "stdcall" || native.DeltaMSType != "int32" ||
		native.CurrentActorOffset != 0x3828 || native.PassStateOffset != 0x350 || native.PassStateSize != 0x1c ||
		native.GridWidthOffset != 0x3b78 || native.GridHeightOffset != 0x3b7c {
		return StaticMovementCapture{}, fmt.Errorf("static movement native provenance is incompatible: %+v", native)
	}
	if len(capture.Probes) == 0 || !capture.Complete {
		return StaticMovementCapture{}, fmt.Errorf("static movement trace is incomplete: probes=%d complete=%t", len(capture.Probes), capture.Complete)
	}
	for index, probe := range capture.Probes {
		if probe.Index != index {
			return StaticMovementCapture{}, fmt.Errorf("static movement probe order %d has native index %d", index, probe.Index)
		}
		if probe.CornerTolerance != battleengine.NativeCornerSlideTolerancePixels {
			return StaticMovementCapture{}, fmt.Errorf("static movement probe %d has corner tolerance %d", index, probe.CornerTolerance)
		}
		if probe.ReturnValue > 1 {
			return StaticMovementCapture{}, fmt.Errorf("static movement probe %d has return value %d", index, probe.ReturnValue)
		}
		state, err := hex.DecodeString(probe.PassStateAfter)
		if err != nil || len(state) != 0x1c {
			return StaticMovementCapture{}, fmt.Errorf("static movement probe %d has invalid pass-state snapshot", index)
		}
	}
	return capture, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func CompareStaticMovementCapture(capture StaticMovementCapture, entry mapdata.CompetitiveMap) (StaticMovementReport, error) {
	if capture.Header.MapID != entry.ID {
		return StaticMovementReport{}, fmt.Errorf("static movement trace map %d does not match catalog map %d", capture.Header.MapID, entry.ID)
	}
	if capture.Header.GridWidth != entry.Battlefield.Width || capture.Header.GridHeight != entry.Battlefield.Height {
		return StaticMovementReport{}, fmt.Errorf("static movement trace grid %dx%d does not match map %dx%d",
			capture.Header.GridWidth, capture.Header.GridHeight, entry.Battlefield.Width, entry.Battlefield.Height)
	}
	if hash := strings.TrimSpace(capture.Header.MapHash); hash != "" {
		actual := hex.EncodeToString(entry.MapHash[:])
		if !strings.EqualFold(hash, actual) {
			return StaticMovementReport{}, fmt.Errorf("static movement trace map hash %s does not match catalog %s", hash, actual)
		}
	}
	grid, err := battleengine.GridFromCompetitiveMap(entry)
	if err != nil {
		return StaticMovementReport{}, err
	}
	report := StaticMovementReport{MapID: entry.ID, ProbeCount: len(capture.Probes)}
	for _, probe := range capture.Probes {
		direction, speed, tick, err := staticProbeInput(probe)
		if err != nil {
			return StaticMovementReport{}, fmt.Errorf("static movement probe %d: %w", probe.Index, err)
		}
		position, ok := integralProbePosition(probe.X, probe.Y)
		if !ok {
			return StaticMovementReport{}, fmt.Errorf("static movement probe %d has non-integral input %.6f,%.6f", probe.Index, probe.X, probe.Y)
		}
		result, err := battleengine.ResolveNativeMovementProbe(battleengine.NativeMovementProbe{
			Grid: grid, Position: position, Direction: direction,
			TickMS: tick, SpeedPixelsPerSecond: speed,
		})
		if err != nil {
			return StaticMovementReport{}, fmt.Errorf("static movement probe %d: %w", probe.Index, err)
		}
		nativePosition, integral := integralProbePosition(probe.OutX, probe.OutY)
		actualReturn := uint32(0)
		if result.Moved {
			actualReturn = 1
		}
		if !integral || nativePosition != result.Position || probe.ReturnValue != actualReturn {
			report.Differences = append(report.Differences, StaticMovementDifference{
				ProbeIndex: probe.Index, Family: probe.Family,
				Expected: fmt.Sprintf("native position=(%.6f,%.6f) return=%d", probe.OutX, probe.OutY, probe.ReturnValue),
				Actual:   fmt.Sprintf("go position=(%d,%d) return=%d", result.Position.X, result.Position.Y, actualReturn),
			})
		}
	}
	return report, nil
}

func staticProbeInput(probe StaticMovementProbe) (battleengine.Direction, uint16, uint32, error) {
	var direction battleengine.Direction
	var scalar float64
	switch probe.Direction {
	case 0:
		direction, scalar = battleengine.DirectionRight, probe.VelocityX
		if probe.VelocityY != 0 || scalar <= 0 {
			return 0, 0, 0, fmt.Errorf("right probe has velocity %.6f,%.6f", probe.VelocityX, probe.VelocityY)
		}
	case 1:
		direction, scalar = battleengine.DirectionUp, -probe.VelocityY
		if probe.VelocityX != 0 || scalar <= 0 {
			return 0, 0, 0, fmt.Errorf("up probe has velocity %.6f,%.6f", probe.VelocityX, probe.VelocityY)
		}
	case 2:
		direction, scalar = battleengine.DirectionLeft, -probe.VelocityX
		if probe.VelocityY != 0 || scalar <= 0 {
			return 0, 0, 0, fmt.Errorf("left probe has velocity %.6f,%.6f", probe.VelocityX, probe.VelocityY)
		}
	case 3:
		direction, scalar = battleengine.DirectionDown, probe.VelocityY
		if probe.VelocityX != 0 || scalar <= 0 {
			return 0, 0, 0, fmt.Errorf("down probe has velocity %.6f,%.6f", probe.VelocityX, probe.VelocityY)
		}
	default:
		return 0, 0, 0, fmt.Errorf("unknown native direction %d", probe.Direction)
	}
	speedFloat := scalar * 40
	speed := uint16(math.Round(speedFloat))
	if speed == 0 || math.Abs(float64(speed)-speedFloat) > 1e-6 {
		return 0, 0, 0, fmt.Errorf("velocity scalar %.6f has no integral pixels/second projection", scalar)
	}
	tick := probe.DeltaMS
	if tick == 0 {
		return 0, 0, 0, fmt.Errorf("delta is not a positive native tick")
	}
	return direction, speed, tick, nil
}

func integralProbePosition(x, y float64) (battleengine.Position, bool) {
	roundedX, roundedY := math.Round(x), math.Round(y)
	if math.Abs(x-roundedX) > 1e-6 || math.Abs(y-roundedY) > 1e-6 ||
		roundedX < math.MinInt32 || roundedX > math.MaxInt32 || roundedY < math.MinInt32 || roundedY > math.MaxInt32 {
		return battleengine.Position{}, false
	}
	return battleengine.Position{X: int32(roundedX), Y: int32(roundedY)}, true
}
