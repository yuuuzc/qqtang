package battleoracle

import (
	"fmt"
	"strings"
	"testing"

	"qqtang/internal/game/mapdata"
)

func TestDecodeAndCompareStaticMovementCapture(t *testing.T) {
	entry := staticTestMap()
	hash := fmt.Sprintf("%x", entry.MapHash)
	trace := strings.Join([]string{
		`{"kind":"collection_header","observed_utc":"now","controller_pid":1,"map_id":1001,"map_hash":"` + hash + `","grid_width":3,"grid_height":3,"client_sha256":"` + strings.Repeat("a", 64) + `","native":` + staticNativeJSON() + `}`,
		`{"kind":"static_movement_probe","index":0,"family":"cell_edge","row":1,"col":1,"x":60,"y":60,"direction":0,"velocity_x":1,"velocity_y":0,"delta_ms":50,"corner_tolerance":6,"out_x":62,"out_y":60,"blocked_x":0,"blocked_y":0,"return_value":1,"pass_state_after":"` + strings.Repeat("00", 0x1c) + `"}`,
		`{"kind":"micro_complete","total":1}`,
		`{"kind":"collection_footer","status":{"completed":true}}`,
	}, "\n")
	capture, err := DecodeStaticMovementCapture(strings.NewReader(trace))
	if err != nil {
		t.Fatal(err)
	}
	report, err := CompareStaticMovementCapture(capture, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Matches() || report.ProbeCount != 1 {
		t.Fatalf("unexpected static report: %+v", report)
	}

	capture.Probes[0].OutX = 61
	report, err = CompareStaticMovementCapture(capture, entry)
	if err != nil {
		t.Fatal(err)
	}
	if report.Matches() || len(report.Differences) != 1 {
		t.Fatalf("native mismatch was not reported: %+v", report)
	}
}

func TestDecodeStaticMovementCaptureRejectsIncompleteAndOutOfOrder(t *testing.T) {
	header := `{"kind":"collection_header","observed_utc":"now","controller_pid":1,"map_id":1001,"map_hash":"","grid_width":3,"grid_height":3,"client_sha256":"` + strings.Repeat("a", 64) + `","native":` + staticNativeJSON() + `}`
	probe := `{"kind":"static_movement_probe","index":1,"family":"cell_edge","x":60,"y":60,"direction":0,"velocity_x":1,"velocity_y":0,"delta_ms":50,"corner_tolerance":6,"out_x":62,"out_y":60,"blocked_x":0,"blocked_y":0,"return_value":1,"pass_state_after":"` + strings.Repeat("00", 0x1c) + `"}`
	if _, err := DecodeStaticMovementCapture(strings.NewReader(header + "\n" + probe + "\n")); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete capture was not rejected: %v", err)
	}
	if _, err := DecodeStaticMovementCapture(strings.NewReader(header + "\n" + probe + "\n" + `{"kind":"micro_complete"}`)); err == nil || !strings.Contains(err.Error(), "order") {
		t.Fatalf("out-of-order capture was not rejected: %v", err)
	}
}

func staticNativeJSON() string {
	return `{"kind":"ready","collector":"qqt-rule1-static-micro-oracle/v2","pid":1,"client_path":"Client.exe","client_base":"0x400000","client_size":1,"movement_resolver_rva":1899536,"movement_resolver_abi":"stdcall","delta_ms_type":"int32","current_actor_offset":14376,"pass_state_offset":848,"pass_state_size":28,"grid_width_offset":15224,"grid_height_offset":15228}`
}

func staticTestMap() mapdata.CompetitiveMap {
	cells := make([]mapdata.CompetitiveBattleCell, 9)
	for index := range cells {
		cells[index].FlamePassable = true
	}
	return mapdata.CompetitiveMap{
		ID: 1001, NativeRule: 1, Rule: mapdata.CompetitiveRuleOrdinary,
		MapHash:     [32]byte{1, 2, 3},
		Battlefield: mapdata.CompetitiveBattlefield{Width: 3, Height: 3, Cells: cells},
	}
}
