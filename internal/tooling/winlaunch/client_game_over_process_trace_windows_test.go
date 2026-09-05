package winlaunch

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestBuildClientGameOverProcessTraceStub(t *testing.T) {
	stub := buildClientGameOverProcessTraceStub(0x10000200, 0x00881E00, nil)
	if !bytes.HasPrefix(stub, []byte{0x9C, 0x60}) {
		t.Fatalf("unexpected GameOver process stub prologue: %X", stub[:2])
	}
	if !bytes.Contains(stub, []byte{0xB9, 0x66, 0x01, 0x00, 0x00, 0xFC, 0xF3, 0xA4}) {
		t.Fatal("GameOver process stub must copy the confirmed 358-byte native object")
	}
	if !bytes.HasSuffix(stub, []byte{0x61, 0x9D, 0x68, 0x00, 0x1E, 0x88, 0x00, 0xC3}) {
		t.Fatalf("GameOver process stub does not preserve state and enter the protected target: %X", stub[len(stub)-8:])
	}
}

func TestBuildClientGameOverProcessTraceStubWithFieldProbe(t *testing.T) {
	probe := &ClientGameOverFieldProbe{
		ReplaceFields: true,
		FieldValue1:   [4]uint32{1, 2, 3, 4},
		FieldValue2:   [4]uint32{11, 22, 33, 44},
	}
	stub := buildClientGameOverProcessTraceStub(0x10000200, 0x00881E00, probe)
	if !bytes.Contains(stub, []byte{0xC6, 0x42, 0x10, 0x04}) {
		t.Fatal("field probe must set the first result FieldCount to four")
	}
	for _, pattern := range [][]byte{
		{0xC7, 0x42, 0x11, 0x01, 0, 0, 0},
		{0xC7, 0x42, 0x1D, 0x04, 0, 0, 0},
		{0xC7, 0x42, 0x21, 0x0B, 0, 0, 0},
		{0xC7, 0x42, 0x2D, 0x2C, 0, 0, 0},
	} {
		if !bytes.Contains(stub, pattern) {
			t.Fatalf("field probe instruction missing: %X", pattern)
		}
	}
}

func TestBuildClientGameOverProcessTraceStubWithGameModeProbe(t *testing.T) {
	gameMode := uint8(2)
	probe := &ClientGameOverFieldProbe{GameMode: &gameMode}
	stub := buildClientGameOverProcessTraceStub(0x10000200, 0x00881E00, probe)
	if !bytes.Contains(stub, []byte{0xC6, 0x82, 0x65, 0x01, 0x00, 0x00, 0x02}) {
		t.Fatal("game-mode probe must replace only native-object byte 357")
	}
	if bytes.Contains(stub, []byte{0xC6, 0x42, 0x10, 0x04}) {
		t.Fatal("game-mode-only probe must not replace result extension fields")
	}
}

func TestDecodeClientGameOverProcessRecord(t *testing.T) {
	record := make([]byte, clientGameOverTraceRecordSize)
	binary.LittleEndian.PutUint32(record[0:4], 1)
	binary.LittleEndian.PutUint32(record[8:12], 1)
	binary.LittleEndian.PutUint32(record[24:28], 0x0FBB)
	native := record[0x40:]
	binary.LittleEndian.PutUint32(native[0:4], 0x12345678)
	native[4] = 1
	binary.LittleEndian.PutUint16(native[5:7], 1)
	native[7] = 0
	binary.LittleEndian.PutUint32(native[8:12], 9)
	binary.LittleEndian.PutUint32(native[12:16], 500)
	native[16] = 2
	binary.LittleEndian.PutUint32(native[17:21], 3)
	binary.LittleEndian.PutUint32(native[21:25], 4)
	binary.LittleEndian.PutUint32(native[33:37], 30)
	binary.LittleEndian.PutUint32(native[37:41], 40)
	native[357] = 26

	tracer := &ClientGameOverProcessTracer{pid: 7, started: time.Now(), entry: 0x00602BAB, target: 0x00881E00}
	capture := tracer.capture(record)
	if capture.Call == nil {
		t.Fatal("capture has no call")
	}
	call := capture.Call
	if call.SchemaID != 0x0FBB || call.Time != 0x12345678 || call.ResultCount != 1 || call.GameMode != 26 {
		t.Fatalf("unexpected GameOver header: %+v", call)
	}
	if len(call.Results) != 1 || call.Results[0].Point != 500 || call.Results[0].Remark != 9 {
		t.Fatalf("unexpected GameOver result: %+v", call.Results)
	}
	if got := call.Results[0].FieldValue1; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("FieldValue1 = %v", got)
	}
	if got := call.Results[0].FieldValue2; len(got) != 2 || got[0] != 30 || got[1] != 40 {
		t.Fatalf("FieldValue2 = %v", got)
	}
}
