package battleoracle

import (
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

func TestDecodeAndComparePassActivationCapture(t *testing.T) {
	lines := []string{
		`{"kind":"collection_header","observed_utc":"now","controller_pid":1,"client_sha256":"` + strings.Repeat("a", 64) + `","native":` + passActivationNativeJSON() + `}`,
	}
	clock := uint32(10_000)
	for index, definition := range expectedPassActivationCases {
		injected := encodedPassActivationState(clock-definition.ElapsedMS, clock-definition.FreshMS, false, 0, 0)
		active := definition.ElapsedMS > 500 && definition.ElapsedMS < 600 && definition.FreshMS < 100
		started, duration := uint32(0), uint32(0)
		if active {
			started, duration = clock, 1000
		}
		result := encodedPassActivationState(clock-definition.ElapsedMS, clock-definition.FreshMS, active, started, duration)
		lines = append(lines, `{"kind":"pass_activation_probe","collector":"qqt-rule1-pass-activation-micro-oracle/v1","index":`+
			strconv.Itoa(index)+`,"name":"`+definition.Name+`","elapsed_ms":`+strconv.Itoa(int(definition.ElapsedMS))+`,"fresh_ms":`+strconv.Itoa(int(definition.FreshMS))+`,"clock":10000,"baseline_state":"`+
			strings.Repeat("00", 28)+`","injected_state":"`+injected+`","result_state":"`+result+`"}`)
	}
	lines = append(lines,
		`{"kind":"micro_complete","collector":"qqt-rule1-pass-activation-micro-oracle/v1","total":7}`,
		`{"kind":"collection_footer","observed_utc":"now","probe_count":7,"status":{"completed":true}}`,
	)
	capture, err := DecodePassActivationCapture(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	report, err := ComparePassActivationCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Matches() || report.ProbeCount != 7 {
		t.Fatalf("unexpected pass activation report: %+v", report)
	}
}

func TestDecodePassActivationCaptureRejectsIncompleteAndBadProvenance(t *testing.T) {
	header := `{"kind":"collection_header","observed_utc":"now","controller_pid":1,"client_sha256":"` + strings.Repeat("a", 64) + `","native":` + passActivationNativeJSON() + `}`
	if _, err := DecodePassActivationCapture(strings.NewReader(header)); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete pass activation capture was not rejected: %v", err)
	}
	bad := strings.Replace(header, `"pass_activate_rva":1913771`, `"pass_activate_rva":1`, 1) + "\n" +
		`{"kind":"micro_complete","collector":"qqt-rule1-pass-activation-micro-oracle/v1","total":7}`
	if _, err := DecodePassActivationCapture(strings.NewReader(bad)); err == nil {
		t.Fatal("bad pass activation provenance was accepted")
	}
}

func encodedPassActivationState(collisionStart, collisionLast uint32, active bool, started, duration uint32) string {
	state := make([]byte, 28)
	if active {
		state[0] = 1
	}
	binary.LittleEndian.PutUint32(state[4:8], 1)
	binary.LittleEndian.PutUint32(state[8:12], 1)
	binary.LittleEndian.PutUint32(state[12:16], collisionStart)
	binary.LittleEndian.PutUint32(state[16:20], collisionLast)
	binary.LittleEndian.PutUint32(state[20:24], started)
	binary.LittleEndian.PutUint32(state[24:28], duration)
	return hex.EncodeToString(state)
}

func passActivationNativeJSON() string {
	return `{"kind":"ready","collector":"qqt-rule1-pass-activation-micro-oracle/v1","pid":1,"client_path":"Client.exe","client_base":"0x400000","client_size":1,"pass_activate_rva":1913771,"pass_activate_abi":"fastcall_ecx_state","scheduler_rva":1899536,"scheduler_abi":"stdcall_natural_game_thread","current_actor_offset":14376,"pass_state_offset":848,"global_root_rva":4221560,"scene_pointer_offset":2004,"scene_clock_offset":19272,"pass_state_size":28,"cases":[{"name":"elapsed_499","elapsed_ms":499,"fresh_ms":0},{"name":"elapsed_500","elapsed_ms":500,"fresh_ms":0},{"name":"elapsed_501","elapsed_ms":501,"fresh_ms":0},{"name":"elapsed_599","elapsed_ms":599,"fresh_ms":0},{"name":"elapsed_600","elapsed_ms":600,"fresh_ms":0},{"name":"fresh_99","elapsed_ms":501,"fresh_ms":99},{"name":"fresh_100","elapsed_ms":501,"fresh_ms":100}]}`
}
