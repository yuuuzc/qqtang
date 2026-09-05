package game

import (
	"encoding/binary"
	"fmt"

	"qqtang/internal/protocol/qqtea"
)

const maxQQTPPPGameplayBatchEntries = 64

var qqtpppGameplayKey = []byte("qqt is the best!")

// GameplayBatch is the plaintext vector carried by QQTPPP Type 2. GameID is
// written in the original client's host byte order; the entry indexes and
// lengths are network ordered. FirstIndex is retained even though every
// captured native movement vector currently writes zero.
type GameplayBatch struct {
	GameID     uint32
	FirstIndex uint16
	Entries    []GameplayBatchEntry
}

// GameplayBatchEntry is one indexed 0x0FBD package in a GameplayBatch.
type GameplayBatchEntry struct {
	Index   uint16
	Package GameplayDataPackage
}

// DecodeQQTPPPGameplayBatch decrypts and parses the native room-peer Type-2
// payload. The recovered key and QQ-TEA layout are fixed properties of the
// original QQTPPP.dll; callers use the result only for inspection and keep the
// original ciphertext unchanged when relaying it.
func DecodeQQTPPPGameplayBatch(ciphertext []byte) (GameplayBatch, error) {
	plaintext, err := qqtea.Decrypt(ciphertext, qqtpppGameplayKey)
	if err != nil {
		return GameplayBatch{}, fmt.Errorf("decrypt QQTPPP gameplay batch: %w", err)
	}
	batch, err := ParseGameplayBatch(plaintext)
	if err != nil {
		return GameplayBatch{}, fmt.Errorf("parse QQTPPP gameplay batch: %w", err)
	}
	return batch, nil
}

// EncodeQQTPPPGameplayBatch builds the same encrypted QQT_MSG_DATA vector
// emitted by QQTPPP's Type-2 peer path.  It is used by server-owned virtual
// players, which have no client process capable of producing the native
// multicast datagram on their behalf.
func EncodeQQTPPPGameplayBatch(batch GameplayBatch) ([]byte, error) {
	if len(batch.Entries) == 0 || len(batch.Entries) > maxQQTPPPGameplayBatchEntries {
		return nil, fmt.Errorf("gameplay batch entry count %d is outside 1..%d", len(batch.Entries), maxQQTPPPGameplayBatchEntries)
	}
	plaintext := make([]byte, 1, 256)
	plaintext[0] = byte(len(batch.Entries))
	plaintext = binary.LittleEndian.AppendUint32(plaintext, batch.GameID)
	plaintext = binary.BigEndian.AppendUint16(plaintext, batch.FirstIndex)
	for index, entry := range batch.Entries {
		compact, err := entry.Package.MarshalNetworkBinary()
		if err != nil {
			return nil, fmt.Errorf("encode gameplay batch entry %d: %w", index, err)
		}
		if len(compact) > int(^uint16(0)) {
			return nil, fmt.Errorf("gameplay batch entry %d length %d exceeds uint16", index, len(compact))
		}
		plaintext = binary.BigEndian.AppendUint16(plaintext, entry.Index)
		plaintext = binary.BigEndian.AppendUint16(plaintext, uint16(len(compact)))
		plaintext = append(plaintext, compact...)
	}
	ciphertext, err := qqtea.Encrypt(plaintext, qqtpppGameplayKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt QQTPPP gameplay batch: %w", err)
	}
	return ciphertext, nil
}

// ParseGameplayBatch parses the plaintext QQT_MSG_DATA vector carried by
// QQTPPP Type 2. It rejects partial and trailing data so a malformed or mixed
// datagram can never be mistaken for a movement-only packet.
func ParseGameplayBatch(plaintext []byte) (GameplayBatch, error) {
	if len(plaintext) < 7 {
		return GameplayBatch{}, fmt.Errorf("gameplay batch header is truncated")
	}
	count := int(plaintext[0])
	if count == 0 || count > maxQQTPPPGameplayBatchEntries {
		return GameplayBatch{}, fmt.Errorf("gameplay batch count %d is outside 1..%d", count, maxQQTPPPGameplayBatchEntries)
	}
	batch := GameplayBatch{
		GameID:     binary.LittleEndian.Uint32(plaintext[1:5]),
		FirstIndex: binary.BigEndian.Uint16(plaintext[5:7]),
		Entries:    make([]GameplayBatchEntry, 0, count),
	}
	offset := 7
	for index := 0; index < count; index++ {
		if len(plaintext)-offset < 4 {
			return GameplayBatch{}, fmt.Errorf("gameplay batch entry %d header is truncated", index)
		}
		entry := GameplayBatchEntry{
			Index: binary.BigEndian.Uint16(plaintext[offset : offset+2]),
		}
		length := int(binary.BigEndian.Uint16(plaintext[offset+2 : offset+4]))
		offset += 4
		if length <= 0 || length > len(plaintext)-offset {
			return GameplayBatch{}, fmt.Errorf("gameplay batch entry %d length %d exceeds remaining %d", index, length, len(plaintext)-offset)
		}
		packet, err := ParseGameplayDataPackage(plaintext[offset : offset+length])
		if err != nil {
			return GameplayBatch{}, fmt.Errorf("decode gameplay batch entry %d: %w", index, err)
		}
		entry.Package = packet
		batch.Entries = append(batch.Entries, entry)
		offset += length
	}
	if offset != len(plaintext) {
		return GameplayBatch{}, fmt.Errorf("gameplay batch has %d trailing bytes", len(plaintext)-offset)
	}
	return batch, nil
}

// GameplayBatchIsMovementOnly reports whether every business message in a
// completely decoded batch is a structurally valid PLAYER_MOVE. Unknown,
// empty, mixed, and malformed batches deliberately return false.
func GameplayBatchIsMovementOnly(batch GameplayBatch) bool {
	if len(batch.Entries) == 0 {
		return false
	}
	for _, entry := range batch.Entries {
		if len(entry.Package.Messages) == 0 {
			return false
		}
		for _, message := range entry.Package.Messages {
			if message.DataID != uint32(PlayerMoveSchema) {
				return false
			}
			if _, err := ParsePlayerMove(message.Data); err != nil {
				return false
			}
		}
	}
	return true
}
