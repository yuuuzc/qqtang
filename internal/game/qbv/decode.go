package qbv

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func DecodeFile(path string) (Recording, error) {
	file, err := os.Open(path)
	if err != nil {
		return Recording{}, err
	}
	defer file.Close()
	recording, err := Decode(file)
	if err != nil {
		return Recording{}, fmt.Errorf("decode QBV %q: %w", path, err)
	}
	return recording, nil
}

func Decode(compressed io.Reader) (Recording, error) {
	stream, err := zlib.NewReader(compressed)
	if err != nil {
		return Recording{}, fmt.Errorf("open zlib stream: %w", err)
	}
	defer stream.Close()
	limited := &io.LimitedReader{R: stream, N: MaxDecodedSize + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return Recording{}, fmt.Errorf("read zlib stream: %w", err)
	}
	if len(data) > MaxDecodedSize {
		return Recording{}, fmt.Errorf("decompressed QBV exceeds %d bytes", MaxDecodedSize)
	}
	return decodeBody(data)
}

func decodeBody(data []byte) (Recording, error) {
	reader := bodyReader{data: data}
	magic, err := reader.bytes(4, "magic")
	if err != nil {
		return Recording{}, err
	}
	if string(magic) != string(Magic[:]) {
		return Recording{}, fmt.Errorf("QBV magic %q, want %q", magic, Magic)
	}
	version, err := reader.uint32("version")
	if err != nil {
		return Recording{}, err
	}
	if version != FormatVersion {
		return Recording{}, fmt.Errorf("QBV version %d, want %d", version, FormatVersion)
	}
	header := HeaderWords{}
	for index, target := range []*uint32{&header.Opaque0, &header.LocalUIN, &header.Opaque2, &header.PlayerCount} {
		*target, err = reader.uint32(fmt.Sprintf("header word %d", index))
		if err != nil {
			return Recording{}, err
		}
	}
	if header.PlayerCount == 0 || header.PlayerCount > MaxPlayers {
		return Recording{}, fmt.Errorf("QBV player count %d is outside 1..%d", header.PlayerCount, MaxPlayers)
	}
	recording := Recording{Version: version, Header: header}
	recording.Bootstrap = make([]BootstrapRecord, 0, int(header.PlayerCount)+2)
	for index := 0; index < int(header.PlayerCount)+2; index++ {
		length, readErr := reader.uint32(fmt.Sprintf("bootstrap %d length", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		schema, readErr := reader.uint32(fmt.Sprintf("bootstrap %d schema", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		payload, readErr := reader.bytes(int(length), fmt.Sprintf("bootstrap %d payload", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		recording.Bootstrap = append(recording.Bootstrap, BootstrapRecord{Schema: schema, Data: append([]byte(nil), payload...)})
	}
	eventCount, err := reader.uint32("event count")
	if err != nil {
		return Recording{}, err
	}
	if eventCount > MaxEvents {
		return Recording{}, fmt.Errorf("QBV event count %d exceeds native limit %d", eventCount, MaxEvents)
	}
	// Each event contains 4+2+2 bytes, a possibly empty body and three DWORDs.
	if uint64(eventCount)*20 > uint64(reader.remaining()) {
		return Recording{}, fmt.Errorf("QBV event count %d needs at least %d bytes, only %d remain", eventCount, uint64(eventCount)*20, reader.remaining())
	}
	recording.Events = make([]Event, 0, int(eventCount))
	var previousTime uint32
	for index := 0; index < int(eventCount); index++ {
		timeMS, readErr := reader.uint32(fmt.Sprintf("event %d time", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		if index != 0 && timeMS < previousTime {
			return Recording{}, fmt.Errorf("QBV event %d time %d precedes %d", index, timeMS, previousTime)
		}
		previousTime = timeMS
		schema, readErr := reader.uint16(fmt.Sprintf("event %d schema", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		length, readErr := reader.uint16(fmt.Sprintf("event %d length", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		payload, readErr := reader.bytes(int(length), fmt.Sprintf("event %d payload", index))
		if readErr != nil {
			return Recording{}, readErr
		}
		event := Event{TimeMS: timeMS, Schema: schema, Data: append([]byte(nil), payload...)}
		for opaqueIndex := range event.Opaque {
			event.Opaque[opaqueIndex], readErr = reader.uint32(fmt.Sprintf("event %d opaque %d", index, opaqueIndex))
			if readErr != nil {
				return Recording{}, readErr
			}
		}
		recording.Events = append(recording.Events, event)
	}
	if reader.remaining() != 0 {
		return Recording{}, fmt.Errorf("QBV has %d trailing decompressed bytes", reader.remaining())
	}
	return recording, nil
}

type bodyReader struct {
	data   []byte
	offset int
}

func (reader *bodyReader) remaining() int {
	return len(reader.data) - reader.offset
}

func (reader *bodyReader) bytes(size int, field string) ([]byte, error) {
	if size < 0 || size > reader.remaining() {
		return nil, fmt.Errorf("QBV %s needs %d bytes, only %d remain", field, size, reader.remaining())
	}
	value := reader.data[reader.offset : reader.offset+size]
	reader.offset += size
	return value, nil
}

func (reader *bodyReader) uint16(field string) (uint16, error) {
	data, err := reader.bytes(2, field)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(data), nil
}

func (reader *bodyReader) uint32(field string) (uint32, error) {
	data, err := reader.bytes(4, field)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data), nil
}
