// Package battleoracle runs versioned original-client golden vectors against
// the restricted deterministic battle engine. It is validation tooling only;
// the live server does not import it and native observations never overwrite
// simulated state.
package battleoracle

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"qqtang/internal/game/battleengine"
)

const SchemaVersion uint16 = 1

type NativeFunction struct {
	Name string `json:"name"`
	RVA  uint32 `json:"rva"`
}

type NativeSource struct {
	ClientSHA256    string           `json:"client_sha256"`
	ImageBase       uint32           `json:"image_base"`
	Collector       string           `json:"collector"`
	CollectorCommit string           `json:"collector_commit,omitempty"`
	Functions       []NativeFunction `json:"functions"`
}

type Suite struct {
	SchemaVersion uint16       `json:"schema_version"`
	Name          string       `json:"name"`
	Source        NativeSource `json:"source"`
	Cases         []Case       `json:"cases"`
}

type Case struct {
	Name    string              `json:"name"`
	MapID   uint32              `json:"map_id,omitempty"`
	MapHash string              `json:"map_hash,omitempty"`
	Config  battleengine.Config `json:"config"`
	Frames  []Frame             `json:"frames"`
}

type Frame struct {
	Actions  []battleengine.Action `json:"actions"`
	Expected Checkpoint            `json:"expected"`
}

// Checkpoint uses pointers to distinguish "not observed by this native
// fixture" from an observed empty collection. This lets the first movement
// oracle compare only actor state while later full-scenario fixtures can also
// assert bombs, flames, field objects, pickups, events and terminal state.
type Checkpoint struct {
	ElapsedMS         *uint32                          `json:"elapsed_ms,omitempty"`
	Actors            *[]battleengine.ActorObservation `json:"actors,omitempty"`
	Bombs             *[]battleengine.Bomb             `json:"bombs,omitempty"`
	Flames            *[]battleengine.Flame            `json:"flames,omitempty"`
	FieldObjects      *[]battleengine.FieldObject      `json:"field_objects,omitempty"`
	ActionProjectiles *[]battleengine.ActionProjectile `json:"action_projectiles,omitempty"`
	Pickups           *[]battleengine.Pickup           `json:"pickups,omitempty"`
	Events            *[]battleengine.Event            `json:"events,omitempty"`
	Outcome           *battleengine.Outcome            `json:"outcome,omitempty"`
}

type Difference struct {
	CaseIndex  int    `json:"case_index"`
	CaseName   string `json:"case_name"`
	FrameIndex int    `json:"frame_index"`
	Field      string `json:"field"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
}

type Report struct {
	SuiteName   string       `json:"suite_name"`
	CaseCount   int          `json:"case_count"`
	FrameCount  int          `json:"frame_count"`
	Differences []Difference `json:"differences"`
}

func (report Report) Matches() bool { return len(report.Differences) == 0 }

func Decode(reader io.Reader) (Suite, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, fmt.Errorf("decode battle oracle suite: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Suite{}, fmt.Errorf("decode battle oracle suite: trailing JSON value")
		}
		return Suite{}, fmt.Errorf("decode battle oracle suite trailing data: %w", err)
	}
	if err := suite.Validate(); err != nil {
		return Suite{}, err
	}
	return suite, nil
}

func (suite Suite) Validate() error {
	if suite.SchemaVersion != SchemaVersion {
		return fmt.Errorf("battle oracle schema %d is unsupported; want %d", suite.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(suite.Name) == "" {
		return fmt.Errorf("battle oracle suite has no name")
	}
	hash := strings.TrimSpace(suite.Source.ClientSHA256)
	if len(hash) != 64 {
		return fmt.Errorf("battle oracle client SHA-256 has length %d, want 64", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("battle oracle client SHA-256 is invalid: %w", err)
	}
	if strings.TrimSpace(suite.Source.Collector) == "" {
		return fmt.Errorf("battle oracle suite has no collector identity")
	}
	if len(suite.Source.Functions) == 0 {
		return fmt.Errorf("battle oracle suite has no native function provenance")
	}
	functionNames := make(map[string]struct{}, len(suite.Source.Functions))
	for index, function := range suite.Source.Functions {
		if strings.TrimSpace(function.Name) == "" || function.RVA == 0 {
			return fmt.Errorf("battle oracle native function %d is incomplete", index)
		}
		if _, duplicate := functionNames[function.Name]; duplicate {
			return fmt.Errorf("battle oracle repeats native function %q", function.Name)
		}
		functionNames[function.Name] = struct{}{}
	}
	if len(suite.Cases) == 0 {
		return fmt.Errorf("battle oracle suite has no cases")
	}
	caseNames := make(map[string]struct{}, len(suite.Cases))
	for caseIndex, testCase := range suite.Cases {
		if strings.TrimSpace(testCase.Name) == "" {
			return fmt.Errorf("battle oracle case %d has no name", caseIndex)
		}
		if _, duplicate := caseNames[testCase.Name]; duplicate {
			return fmt.Errorf("battle oracle repeats case %q", testCase.Name)
		}
		caseNames[testCase.Name] = struct{}{}
		if len(testCase.Frames) == 0 {
			return fmt.Errorf("battle oracle case %q has no frames", testCase.Name)
		}
		if _, err := battleengine.New(testCase.Config); err != nil {
			return fmt.Errorf("battle oracle case %q config: %w", testCase.Name, err)
		}
		for frameIndex, frame := range testCase.Frames {
			if frame.Expected.empty() {
				return fmt.Errorf("battle oracle case %q frame %d has no native assertions", testCase.Name, frameIndex)
			}
		}
	}
	return nil
}

func (checkpoint Checkpoint) empty() bool {
	return checkpoint.ElapsedMS == nil && checkpoint.Actors == nil && checkpoint.Bombs == nil &&
		checkpoint.Flames == nil && checkpoint.FieldObjects == nil && checkpoint.ActionProjectiles == nil &&
		checkpoint.Pickups == nil && checkpoint.Events == nil && checkpoint.Outcome == nil
}

func Run(suite Suite) (Report, error) {
	if err := suite.Validate(); err != nil {
		return Report{}, err
	}
	report := Report{SuiteName: suite.Name, CaseCount: len(suite.Cases)}
	for caseIndex, testCase := range suite.Cases {
		engine, err := battleengine.New(testCase.Config)
		if err != nil {
			return Report{}, fmt.Errorf("create battle oracle case %q: %w", testCase.Name, err)
		}
		observerID := testCase.Config.Participants[0].PlayerID
		for frameIndex, frame := range testCase.Frames {
			report.FrameCount++
			events, err := engine.Step(frame.Actions)
			if err != nil {
				return Report{}, fmt.Errorf("battle oracle case %q frame %d: %w", testCase.Name, frameIndex, err)
			}
			actual, err := Capture(engine, observerID, events)
			if err != nil {
				return Report{}, fmt.Errorf("capture battle oracle case %q frame %d: %w", testCase.Name, frameIndex, err)
			}
			report.Differences = append(report.Differences, compareCheckpoint(caseIndex, testCase.Name, frameIndex, frame.Expected, actual)...)
		}
	}
	return report, nil
}

func Capture(engine *battleengine.Engine, observerID uint16, events []battleengine.Event) (Checkpoint, error) {
	observation, err := engine.Observation(observerID)
	if err != nil {
		return Checkpoint{}, err
	}
	actors := append([]battleengine.ActorObservation{}, observation.Actors...)
	sort.Slice(actors, func(i, j int) bool { return actors[i].PlayerID < actors[j].PlayerID })
	bombs := append([]battleengine.Bomb{}, observation.Bombs...)
	sort.Slice(bombs, func(i, j int) bool { return bombs[i].ID < bombs[j].ID })
	flames := append([]battleengine.Flame{}, observation.Flames...)
	sort.Slice(flames, func(i, j int) bool {
		if flames[i].Cell.Row != flames[j].Cell.Row {
			return flames[i].Cell.Row < flames[j].Cell.Row
		}
		if flames[i].Cell.Col != flames[j].Cell.Col {
			return flames[i].Cell.Col < flames[j].Cell.Col
		}
		return flames[i].OwnerID < flames[j].OwnerID
	})
	fieldObjects := append([]battleengine.FieldObject{}, observation.FieldObjects...)
	sort.Slice(fieldObjects, func(i, j int) bool { return fieldObjects[i].ID < fieldObjects[j].ID })
	projectiles := append([]battleengine.ActionProjectile{}, observation.ActionProjectiles...)
	sort.Slice(projectiles, func(i, j int) bool { return projectiles[i].ID < projectiles[j].ID })
	pickups := append([]battleengine.Pickup{}, observation.Pickups...)
	sort.Slice(pickups, func(i, j int) bool {
		if pickups[i].Cell.Row != pickups[j].Cell.Row {
			return pickups[i].Cell.Row < pickups[j].Cell.Row
		}
		if pickups[i].Cell.Col != pickups[j].Cell.Col {
			return pickups[i].Cell.Col < pickups[j].Cell.Col
		}
		return pickups[i].SceneID < pickups[j].SceneID
	})
	eventCopy := append([]battleengine.Event{}, events...)
	elapsed := observation.ElapsedMS
	outcome := observation.Outcome
	return Checkpoint{
		ElapsedMS: &elapsed, Actors: &actors, Bombs: &bombs, Flames: &flames,
		FieldObjects: &fieldObjects, ActionProjectiles: &projectiles, Pickups: &pickups,
		Events: &eventCopy, Outcome: &outcome,
	}, nil
}

func compareCheckpoint(caseIndex int, caseName string, frameIndex int, expected, actual Checkpoint) []Difference {
	differences := make([]Difference, 0)
	compare := func(field string, want, got any) {
		if reflect.DeepEqual(want, got) {
			return
		}
		differences = append(differences, Difference{
			CaseIndex: caseIndex, CaseName: caseName, FrameIndex: frameIndex, Field: field,
			Expected: compactJSON(want), Actual: compactJSON(got),
		})
	}
	if expected.ElapsedMS != nil {
		compare("elapsed_ms", *expected.ElapsedMS, *actual.ElapsedMS)
	}
	if expected.Actors != nil {
		compare("actors", *expected.Actors, *actual.Actors)
	}
	if expected.Bombs != nil {
		compare("bombs", *expected.Bombs, *actual.Bombs)
	}
	if expected.Flames != nil {
		compare("flames", *expected.Flames, *actual.Flames)
	}
	if expected.FieldObjects != nil {
		compare("field_objects", *expected.FieldObjects, *actual.FieldObjects)
	}
	if expected.ActionProjectiles != nil {
		compare("action_projectiles", *expected.ActionProjectiles, *actual.ActionProjectiles)
	}
	if expected.Pickups != nil {
		compare("pickups", *expected.Pickups, *actual.Pickups)
	}
	if expected.Events != nil {
		compare("events", *expected.Events, *actual.Events)
	}
	if expected.Outcome != nil {
		compare("outcome", *expected.Outcome, *actual.Outcome)
	}
	return differences
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return string(data)
	}
	return compact.String()
}
