package capture

import (
	"encoding/hex"
	"strings"
	"time"
)

// Record is the stable JSON-lines representation of one observed datagram or
// TCP read/write. Fields unavailable to a pure Go socket server are explicit.
type Record struct {
	Time          time.Time `json:"time"`
	ConnectionID  string    `json:"connection_id"`
	Direction     string    `json:"direction"`
	Network       string    `json:"network"`
	LocalAddress  string    `json:"local_address"`
	RemoteAddress string    `json:"remote_address"`
	Length        int       `json:"length"`
	Hex           string    `json:"hex"`
	ASCIIPreview  string    `json:"ascii_preview"`
	CallThread    string    `json:"call_thread,omitempty"`
	CallSite      string    `json:"call_site,omitempty"`
}

func NewRecord(now time.Time, id, direction, network, local, remote string, data []byte) Record {
	return Record{
		Time:          now.UTC(),
		ConnectionID:  id,
		Direction:     direction,
		Network:       network,
		LocalAddress:  local,
		RemoteAddress: remote,
		Length:        len(data),
		Hex:           hex.EncodeToString(data),
		ASCIIPreview:  asciiPreview(data, 128),
		CallThread:    "unavailable-server-side",
		CallSite:      "unavailable-server-side",
	}
}

func asciiPreview(data []byte, limit int) string {
	if len(data) > limit {
		data = data[:limit]
	}
	var b strings.Builder
	for _, c := range data {
		if c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}
