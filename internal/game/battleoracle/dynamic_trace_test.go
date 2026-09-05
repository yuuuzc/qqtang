package battleoracle

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecodeAndCompareDynamicMovementCapture(t *testing.T) {
	entry := staticTestMap()
	hash := fmt.Sprintf("%x", entry.MapHash)
	before := "00000000ffffffffffffffff00000000000000000000000000000000"
	after := "000000000c0000000400000064000000640000000000000000000000"
	trace := strings.Join([]string{
		`{"kind":"collection_header","collector":"qqt-rule1-movement-oracle-host/v2","observed_utc":"now","controller_pid":1,"map_id":1001,"map_hash":"` + hash + `","client_sha256":"` + strings.Repeat("a", 64) + `","native":` + dynamicNativeJSON() + `}`,
		`{"kind":"movement_call","observed_utc":"now","sequence":1,"timestamp_ms":1,"thread_id":2,"caller":"Client.exe+0x1cfaa3","manager":"0x1","x":60,"y":60,"actor_context":"0x0","direction":0,"velocity_x":1,"velocity_y":0,"delta_ms":50,"corner_tolerance":6,"pass_state_before":{"actor":"0x2","bytes":"` + before + `"},"dynamic_cell_lookups":[{"caller":"Client.exe+0x1b7f7f","container":"0x3","row":1,"col":2,"result":"0x4","object_count":0,"object_types":[],"sequence":1}],"dynamic_boundary_calls":[{"caller":"Client.exe+0x1b7b2e","corner_index":0,"x1":1,"y1":2,"x2":3,"y2":4,"row":1,"col":2,"direction":0,"ignore_dynamic":0,"return_value":1,"collision_kind":0,"sequence":2}],"pass_touch_calls":[{"caller":"Client.exe+0x1b7bfb","state":"0x5","row":4,"col":12,"state_before":"` + before + `","state_after":"` + after + `","sequence":3}],"out_x":62,"out_y":60,"blocked_x":0,"blocked_y":0,"return_value":1,"pass_state_after":{"actor":"0x2","bytes":"` + after + `"}}`,
		`{"kind":"collection_complete","collector":"qqt-rule1-movement-oracle-host/v2","observed_utc":"now","movement_calls":1}`,
	}, "\n")
	capture, err := DecodeDynamicMovementCapture(strings.NewReader(trace))
	if err != nil {
		t.Fatal(err)
	}
	report, err := CompareDynamicMovementCapture(capture, entry, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Matches() || report.PassTouchCalls != 1 || report.BoundaryCalls != 1 {
		t.Fatalf("unexpected dynamic report: %+v", report)
	}
}

func TestDecodeDynamicMovementCaptureRejectsIncompleteAndBadProvenance(t *testing.T) {
	header := `{"kind":"collection_header","collector":"qqt-rule1-movement-oracle-host/v2","observed_utc":"now","controller_pid":1,"map_id":1001,"map_hash":"` + strings.Repeat("b", 64) + `","client_sha256":"` + strings.Repeat("a", 64) + `","native":` + dynamicNativeJSON() + `}`
	if _, err := DecodeDynamicMovementCapture(strings.NewReader(header)); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete dynamic capture was not rejected: %v", err)
	}
	bad := strings.Replace(header, "qqt-rule1-movement-oracle/v4", "qqt-rule1-movement-oracle/v3", 1) + "\n" +
		`{"kind":"movement_call","observed_utc":"now","sequence":1,"timestamp_ms":1,"thread_id":2,"caller":"x","manager":"x","x":1,"y":1,"actor_context":"x","direction":0,"velocity_x":1,"velocity_y":0,"delta_ms":50,"corner_tolerance":6,"out_x":1,"out_y":1,"blocked_x":0,"blocked_y":0,"return_value":1}` + "\n" +
		`{"kind":"collection_complete","collector":"qqt-rule1-movement-oracle-host/v2","observed_utc":"now","movement_calls":1}`
	if _, err := DecodeDynamicMovementCapture(strings.NewReader(bad)); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("bad dynamic provenance was not rejected: %v", err)
	}
}

func dynamicNativeJSON() string {
	return `{"kind":"ready","collector":"qqt-rule1-movement-oracle/v4","pid":1,"architecture":"ia32","client_path":"Client.exe","client_base":"0x400000","client_size":1,"movement_resolver_rva":1899536,"movement_resolver_abi":"stdcall","delta_ms_type":"int32","movement_resolver":"0x5cfc10","cell_resolver_rva":1800601,"cell_classifier_rva":1801671,"dynamic_boundary_rva":1802076,"dynamic_cell_lookup_rva":1915859,"pass_touch_rva":1913692,"pass_activate_rva":1913771,"current_actor_offset":14376,"pass_state_offset":848,"pass_state_size":28}`
}
