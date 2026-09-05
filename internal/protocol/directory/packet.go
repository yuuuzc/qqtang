package directory

import (
	"encoding/binary"
	"fmt"
	"io"

	"qqtang/internal/protocol/qqtea"
)

var (
	LocalKey = []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F}
	LocalST  = []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F}
	DirectoryResponseInnerHeader = []byte{0x01, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x06, 0xFF, 0xFF, 0x00, 0xFE}
)

const directoryInnerHeaderSize = 14

// BuildLocalResponse builds the same length/sequence/UIN/ST envelope observed
// in the client's deterministic local 0x0133 request. The schema-0x0816 body
// is supplied separately so the envelope can be validated before inventing
// any server-list fields.
func BuildLocalResponse(uin uint32, sequence uint32, payload []byte) ([]byte, error) {
	return buildLocalResponse(uin, sequence, DirectoryResponseInnerHeader, payload, nil)
}

// BuildLocalResponseWithReader is the deterministic test form. entropy is
// passed to QQ-TEA for padding and salt generation.
func BuildLocalResponseWithReader(uin uint32, sequence uint32, payload []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalResponse(uin, sequence, DirectoryResponseInnerHeader, payload, entropy)
}

func buildLocalResponse(uin uint32, sequence uint32, innerHeader, payload []byte, entropy io.Reader) ([]byte, error) {
	if len(innerHeader) != directoryInnerHeaderSize {
		return nil, fmt.Errorf("directory response inner header length %d, want %d", len(innerHeader), directoryInnerHeaderSize)
	}
	plaintext := make([]byte, 0, len(innerHeader)+len(payload))
	plaintext = append(plaintext, innerHeader...)
	plaintext = append(plaintext, payload...)
	var (
		ciphertext []byte
		err        error
	)
	if entropy == nil {
		ciphertext, err = qqtea.Encrypt(plaintext, LocalKey)
	} else {
		ciphertext, err = qqtea.EncryptWithReader(plaintext, LocalKey, entropy)
	}
	if err != nil {
		return nil, err
	}
	total := 18 + len(LocalST) + len(ciphertext)
	packet := make([]byte, total)
	binary.BigEndian.PutUint32(packet[0:4], uint32(total))
	binary.BigEndian.PutUint32(packet[4:8], sequence)
	// Bytes 8..11 are the observed fixed directory route marker.
	copy(packet[8:12], []byte{0x00, 0x00, 0xFF, 0xFF})
	binary.BigEndian.PutUint32(packet[12:16], uin)
	packet[16] = 1
	packet[17] = byte(len(LocalST))
	copy(packet[18:50], LocalST)
	copy(packet[50:], ciphertext)
	return packet, nil
}
