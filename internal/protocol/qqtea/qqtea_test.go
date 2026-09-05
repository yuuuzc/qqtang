package qqtea

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	for _, length := range []int{0, 1, 7, 8, 9, 31, 136, 1024} {
		plaintext := make([]byte, length)
		for index := range plaintext {
			plaintext[index] = byte(index*37 + 11)
		}
		entropy := bytes.NewReader(bytes.Repeat([]byte{0xa5, 0x5a, 0xc3, 0x3c}, 4))
		ciphertext, err := EncryptWithReader(plaintext, key, entropy)
		if err != nil {
			t.Fatalf("length %d encrypt: %v", length, err)
		}
		decoded, err := Decrypt(ciphertext, key)
		if err != nil {
			t.Fatalf("length %d decrypt: %v", length, err)
		}
		if !bytes.Equal(decoded, plaintext) {
			t.Fatalf("length %d round trip mismatch", length)
		}
	}
}

func TestEncryptReproducesCapturedDirectoryPacket(t *testing.T) {
	key := mustDecodeHex(t, "101112131415161718191a1b1c1d1e1f")
	ciphertext := mustDecodeHex(t, "03a623373a01d16622598ed56a997abe55ae47c868139bab40ddae41f366e473e0b52b502fedcd0c26502eaa67b2588faa77bfefb5049a15f4502d206b319f4d10abb22141d86e99cbd59b74937bafc279bc1d90497aa11ef9011aabe05feca656e6ca7fd5ca056b5ac92685382111552bd2b7afabe4bcd499eac14a0956ed820a8060e4c73f0851fb2d0a639d58dd76d876473aa9969ad4")
	plaintext, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatal(err)
	}
	formatted := decryptFormatted(ciphertext, key)
	padding := int(formatted[0] & 0x07)
	entropy := bytes.NewReader(formatted[:padding+3])
	reproduced, err := EncryptWithReader(plaintext, key, entropy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reproduced, ciphertext) {
		t.Fatalf("captured ciphertext mismatch\n got: %x\nwant: %x", reproduced, ciphertext)
	}
}

func TestEncryptRejectsInvalidInputs(t *testing.T) {
	if _, err := EncryptWithReader(nil, make([]byte, 15), bytes.NewReader(nil)); err == nil {
		t.Fatal("expected invalid key error")
	}
	if _, err := EncryptWithReader(nil, make([]byte, 16), nil); err == nil {
		t.Fatal("expected nil entropy error")
	}
	if _, err := EncryptWithReader(nil, make([]byte, 16), bytes.NewReader(nil)); err == nil {
		t.Fatal("expected short entropy error")
	}
}

func decryptFormatted(ciphertext, key []byte) []byte {
	formatted := make([]byte, len(ciphertext))
	var previousCipher [blockSize]byte
	var previousIntermediate [blockSize]byte
	for offset := 0; offset < len(ciphertext); offset += blockSize {
		var mixed [blockSize]byte
		for index := range mixed {
			mixed[index] = ciphertext[offset+index] ^ previousIntermediate[index]
		}
		intermediate := decryptBlock(mixed, key)
		for index := range intermediate {
			formatted[offset+index] = intermediate[index] ^ previousCipher[index]
		}
		previousIntermediate = intermediate
		copy(previousCipher[:], ciphertext[offset:offset+blockSize])
	}
	return formatted
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
