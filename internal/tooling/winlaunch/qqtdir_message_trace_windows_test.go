package winlaunch

import (
	"encoding/binary"
	"testing"
)

func TestDecodeQQTDirMessageRecord(t *testing.T) {
	record := make([]byte, qqtDirMessageRecordSize)
	values := []uint32{
		3, 2, 0x06A71234, 0x1234, 0x00ABCDEF,
		0x8123, 0x11223344, 0x55667788, 0xFFFFFFFF,
	}
	for index, value := range values {
		binary.LittleEndian.PutUint32(record[index*4:index*4+4], value)
	}
	call := decodeQQTDirMessageRecord(record, 17)
	if call.ElapsedMS != 17 || call.CallsEntered != 3 || call.CallsCompleted != 2 {
		t.Fatalf("unexpected counters: %+v", call)
	}
	if call.Caller != "0x06A71234" || call.ThreadID != 0x1234 || call.Window != "0x00ABCDEF" {
		t.Fatalf("unexpected origin fields: %+v", call)
	}
	if call.Message != 0x8123 || call.WParam != "0x11223344" || call.LParam != "0x55667788" {
		t.Fatalf("unexpected message fields: %+v", call)
	}
	if call.Result != -1 {
		t.Fatalf("unexpected result: %d", call.Result)
	}
}
