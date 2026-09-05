package qqtea

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	blockSize = 8
	delta     = uint32(0x9E3779B9)
	rounds    = 16
)

// Encrypt formats and encrypts plaintext with the legacy Tencent QQ-TEA
// chaining mode. Cryptographic padding bytes come from crypto/rand.Reader.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	return EncryptWithReader(plaintext, key, rand.Reader)
}

// EncryptWithReader is Encrypt with an explicit entropy source. It exists so
// protocol tests and capture reproduction can use deterministic padding.
func EncryptWithReader(plaintext, key []byte, entropy io.Reader) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("QQ-TEA key length = %d, want 16", len(key))
	}
	if entropy == nil {
		return nil, fmt.Errorf("QQ-TEA entropy reader is nil")
	}
	padding := (blockSize - ((len(plaintext) + 10) % blockSize)) % blockSize
	formatted := make([]byte, 1+padding+2+len(plaintext)+7)
	randomPrefix := make([]byte, padding+3)
	if _, err := io.ReadFull(entropy, randomPrefix); err != nil {
		return nil, fmt.Errorf("QQ-TEA entropy: %w", err)
	}
	formatted[0] = randomPrefix[0]&0xf8 | byte(padding)
	copy(formatted[1:padding+3], randomPrefix[1:])
	copy(formatted[padding+3:], plaintext)

	ciphertext := make([]byte, len(formatted))
	var previousCipher [blockSize]byte
	var previousIntermediate [blockSize]byte
	for offset := 0; offset < len(formatted); offset += blockSize {
		var intermediate [blockSize]byte
		for index := range intermediate {
			intermediate[index] = formatted[offset+index] ^ previousCipher[index]
		}
		encrypted := encryptBlock(intermediate, key)
		for index := range encrypted {
			ciphertext[offset+index] = encrypted[index] ^ previousIntermediate[index]
		}
		previousIntermediate = intermediate
		copy(previousCipher[:], ciphertext[offset:offset+blockSize])
	}
	return ciphertext, nil
}

// Decrypt implements the legacy Tencent QQ-TEA block chaining used by this
// client. The returned slice excludes the random padding, two salt bytes, and
// seven trailing zero bytes from the formatted plaintext.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("QQ-TEA key length = %d, want 16", len(key))
	}
	if len(ciphertext) < 16 || len(ciphertext)%blockSize != 0 {
		return nil, fmt.Errorf("QQ-TEA ciphertext length = %d, want a multiple of 8 and at least 16", len(ciphertext))
	}
	formatted := make([]byte, len(ciphertext))
	var previousCipher [blockSize]byte
	var previousPlain [blockSize]byte
	for offset := 0; offset < len(ciphertext); offset += blockSize {
		var mixed [blockSize]byte
		for index := range mixed {
			mixed[index] = ciphertext[offset+index] ^ previousPlain[index]
		}
		plainBlock := decryptBlock(mixed, key)
		for index := range plainBlock {
			formatted[offset+index] = plainBlock[index] ^ previousCipher[index]
		}
		previousPlain = plainBlock
		copy(previousCipher[:], ciphertext[offset:offset+blockSize])
	}
	padding := int(formatted[0] & 0x07)
	start := 1 + padding + 2
	end := len(formatted) - 7
	if start > end {
		return nil, fmt.Errorf("QQ-TEA padding %d exceeds formatted plaintext", padding)
	}
	for index := end; index < len(formatted); index++ {
		if formatted[index] != 0 {
			return nil, fmt.Errorf("QQ-TEA trailing zero validation failed at %d", index)
		}
	}
	return append([]byte(nil), formatted[start:end]...), nil
}

func decryptBlock(block [blockSize]byte, key []byte) [blockSize]byte {
	left := binary.BigEndian.Uint32(block[0:4])
	right := binary.BigEndian.Uint32(block[4:8])
	key0 := binary.BigEndian.Uint32(key[0:4])
	key1 := binary.BigEndian.Uint32(key[4:8])
	key2 := binary.BigEndian.Uint32(key[8:12])
	key3 := binary.BigEndian.Uint32(key[12:16])
	sum := uint32(0xE3779B90) // delta * 16 modulo 2^32
	for round := 0; round < rounds; round++ {
		right -= ((left << 4) + key2) ^ (left + sum) ^ ((left >> 5) + key3)
		left -= ((right << 4) + key0) ^ (right + sum) ^ ((right >> 5) + key1)
		sum -= delta
	}
	var plaintext [blockSize]byte
	binary.BigEndian.PutUint32(plaintext[0:4], left)
	binary.BigEndian.PutUint32(plaintext[4:8], right)
	return plaintext
}

func encryptBlock(block [blockSize]byte, key []byte) [blockSize]byte {
	left := binary.BigEndian.Uint32(block[0:4])
	right := binary.BigEndian.Uint32(block[4:8])
	key0 := binary.BigEndian.Uint32(key[0:4])
	key1 := binary.BigEndian.Uint32(key[4:8])
	key2 := binary.BigEndian.Uint32(key[8:12])
	key3 := binary.BigEndian.Uint32(key[12:16])
	var sum uint32
	for round := 0; round < rounds; round++ {
		sum += delta
		left += ((right << 4) + key0) ^ (right + sum) ^ ((right >> 5) + key1)
		right += ((left << 4) + key2) ^ (left + sum) ^ ((left >> 5) + key3)
	}
	var ciphertext [blockSize]byte
	binary.BigEndian.PutUint32(ciphertext[0:4], left)
	binary.BigEndian.PutUint32(ciphertext[4:8], right)
	return ciphertext
}
