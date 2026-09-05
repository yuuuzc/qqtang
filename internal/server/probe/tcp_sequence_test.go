package probe

import (
	"io"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/capture"
)

func TestTCPSequenceWritesInventoryPreludeBeforeProtocolResponse(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()

	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{capture: captureWriter, logWriter: io.Discard}

	want := []byte("inventoryresponse")
	received := make(chan []byte, 1)
	go func() {
		data := make([]byte, len(want))
		if _, err := io.ReadFull(clientSide, data); err != nil {
			received <- nil
			return
		}
		received <- data
	}()

	ok := server.deliverTCPSequence(tcpSequenceDelivery{
		connection:           serverSide,
		connectionID:         "combine",
		local:                "127.0.0.1:18000",
		remote:               "127.0.0.1:50000",
		beforeResponse:       []byte("inventory"),
		beforeResponseResult: "combine_inventory_refresh_before_result",
		response:             []byte("response"),
		result:               "combine_result",
	})
	if !ok {
		t.Fatal("sequence delivery failed")
	}
	if got := <-received; string(got) != string(want) {
		t.Fatalf("wire order = %q, want %q", got, want)
	}
}
