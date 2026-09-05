package winlaunch

import "testing"

func TestDecodeQQTPPPNetworkOrderIPv4UsesWinsockMemoryOrder(t *testing.T) {
	text, raw := decodeQQTPPPNetworkOrderIPv4([]byte{127, 0, 0, 1})
	if text != "127.0.0.1" || raw != "0x0100007F" {
		t.Fatalf("decodeQQTPPPNetworkOrderIPv4 = %q, %q", text, raw)
	}
}

func TestDecodeQQTPPPHostOrderIPv4FormatsNumericULong(t *testing.T) {
	text, raw := decodeQQTPPPHostOrderIPv4([]byte{1, 0, 0, 127})
	if text != "127.0.0.1" || raw != "0x7F000001" {
		t.Fatalf("decodeQQTPPPHostOrderIPv4 = %q, %q", text, raw)
	}
}

func TestQQTPPPPeerStateNamesAreEvidenceBounded(t *testing.T) {
	for state, want := range map[uint32]string{
		0: "empty",
		1: "registered_waiting_negotiation",
		2: "udp_ok_requested_waiting_candidate",
		3: "handshake_retry",
		5: "established",
		4: "unknown",
	} {
		if got := qqtPPPPeerStateName(state); got != want {
			t.Fatalf("state %d name = %q, want %q", state, got, want)
		}
	}
}
