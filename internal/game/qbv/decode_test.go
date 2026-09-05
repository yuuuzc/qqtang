package qbv

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

func TestDecodeNativeContainer(t *testing.T) {
	body := make([]byte, 0, 128)
	body = append(body, Magic[:]...)
	body = binary.LittleEndian.AppendUint32(body, FormatVersion)
	body = binary.LittleEndian.AppendUint32(body, 0x11223344)
	body = binary.LittleEndian.AppendUint32(body, 1_000_001)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 1)
	for _, bootstrap := range []BootstrapRecord{
		{Schema: 0x0FA1, Data: []byte{1, 2}},
		{Schema: 0x0FBB, Data: []byte{3}},
		{Schema: 0x042A, Data: []byte{4, 5, 6}},
	} {
		body = binary.LittleEndian.AppendUint32(body, uint32(len(bootstrap.Data)))
		body = binary.LittleEndian.AppendUint32(body, bootstrap.Schema)
		body = append(body, bootstrap.Data...)
	}
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 3000)
	body = binary.LittleEndian.AppendUint16(body, 0x0FA3)
	body = binary.LittleEndian.AppendUint16(body, 3)
	body = append(body, 7, 8, 9)
	body = binary.LittleEndian.AppendUint32(body, 10)
	body = binary.LittleEndian.AppendUint32(body, 11)
	body = binary.LittleEndian.AppendUint32(body, 12)

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	recording, err := Decode(bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if recording.Header.LocalUIN != 1_000_001 || recording.Header.PlayerCount != 1 || len(recording.Bootstrap) != 3 || len(recording.Events) != 1 {
		t.Fatalf("unexpected recording: %+v", recording)
	}
	event := recording.Events[0]
	if event.TimeMS != 3000 || event.Schema != 0x0FA3 || !bytes.Equal(event.Data, []byte{7, 8, 9}) || event.Opaque != [3]uint32{10, 11, 12} {
		t.Fatalf("unexpected event: %+v", event)
	}
	if recording.DurationMS() != 3000 {
		t.Fatalf("duration = %d, want 3000", recording.DurationMS())
	}
}
