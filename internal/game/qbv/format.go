// Package qbv decodes the original client's native .qbv battle recordings.
//
// The format is a zlib stream. Its decompressed body is little-endian and
// starts with the four bytes "vaqq" followed by version 1. Battle message
// bodies remain in their native network byte order and are intentionally kept
// opaque here so protocol-specific decoders can consume them without a second
// serialization.
package qbv

const (
	FormatVersion  uint32 = 1
	MaxPlayers            = 8
	MaxEvents             = 120_000
	MaxDecodedSize        = 4 * 1024 * 1024
)

var Magic = [4]byte{'v', 'a', 'q', 'q'}

// HeaderWords are the four DWORDs written verbatim from the native replay
// object after the format version. Only LocalUIN and PlayerCount have been
// identified from source and a live recording; the other two values stay
// explicitly opaque until their consumers are proven.
type HeaderWords struct {
	Opaque0     uint32
	LocalUIN    uint32
	Opaque2     uint32
	PlayerCount uint32
}

// BootstrapRecord is one of the two match bootstrap objects followed by one
// object per player. Schema and Data are the native object's serialized ID and
// payload; unlike timed battle events, these objects are not assumed to use a
// particular protocol encoding.
type BootstrapRecord struct {
	Schema uint32
	Data   []byte
}

// Event is the exact timed record loaded by Client+0x1F2EA3. The three trailing
// DWORDs are preserved losslessly but remain opaque: their values are not
// required to delimit or dispatch the native battle message.
type Event struct {
	TimeMS uint32
	Schema uint16
	Data   []byte
	Opaque [3]uint32
}

type Recording struct {
	Version   uint32
	Header    HeaderWords
	Bootstrap []BootstrapRecord
	Events    []Event
}

func (recording Recording) DurationMS() uint32 {
	if len(recording.Events) == 0 {
		return 0
	}
	return recording.Events[len(recording.Events)-1].TimeMS
}
