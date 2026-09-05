// Package accountauth implements the local account password verifier and the
// short challenge-response used by the Windows login compatibility helper.
// Passwords never enter the legacy QQTang game protocol and are never stored
// in plaintext.
package accountauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	DefaultPassword   = "123456"
	DefaultIterations = 210_000
	SaltSize          = 16
	KeySize           = 32
	NonceSize         = 32
)

type Verifier struct {
	Iterations uint32
	Salt       []byte
	StoredKey  []byte
}

func ValidatePassword(password string) error {
	if len(password) < 6 || len(password) > 16 {
		return fmt.Errorf("local account password must be 6-16 ASCII characters")
	}
	for index := range password {
		if password[index] < 0x21 || password[index] > 0x7e {
			return fmt.Errorf("local account password must contain printable ASCII characters only")
		}
	}
	return nil
}

func NewVerifier(password string) (Verifier, error) {
	return NewVerifierWithReader(password, rand.Reader)
}

func NewVerifierWithReader(password string, entropy io.Reader) (Verifier, error) {
	if err := ValidatePassword(password); err != nil {
		return Verifier{}, err
	}
	if entropy == nil {
		return Verifier{}, fmt.Errorf("password salt entropy reader is nil")
	}
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(entropy, salt); err != nil {
		return Verifier{}, fmt.Errorf("generate local account password salt: %w", err)
	}
	passwordBytes := []byte(password)
	saltedPassword := DeriveKey(passwordBytes, salt, DefaultIterations)
	clientKey := hmacBytes(saltedPassword, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)
	clear(passwordBytes)
	clear(saltedPassword)
	clear(clientKey)
	return Verifier{Iterations: DefaultIterations, Salt: salt, StoredKey: storedKey[:]}, nil
}

func (verifier Verifier) Validate() error {
	if verifier.Iterations < 100_000 || verifier.Iterations > 2_000_000 {
		return fmt.Errorf("local account password iterations are outside the safe range")
	}
	if len(verifier.Salt) < SaltSize || len(verifier.Salt) > 64 {
		return fmt.Errorf("local account password salt is invalid")
	}
	if len(verifier.StoredKey) != KeySize {
		return fmt.Errorf("local account password verifier is invalid")
	}
	return nil
}

func DeriveKey(password, salt []byte, iterations uint32) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	result := append([]byte(nil), u...)
	for round := uint32(1); round < iterations; round++ {
		mac = hmac.New(sha256.New, password)
		mac.Write(u)
		u = mac.Sum(nil)
		for index := range result {
			result[index] ^= u[index]
		}
	}
	return result
}

func ClientProof(password, salt []byte, iterations uint32, nonce []byte, uin uint32) ([]byte, error) {
	if len(password) < 6 || len(password) > 16 || iterations < 100_000 || iterations > 2_000_000 || len(salt) < SaltSize || len(salt) > 64 || len(nonce) != NonceSize || uin == 0 {
		return nil, fmt.Errorf("local account proof parameters are invalid")
	}
	for _, value := range password {
		if value < 0x21 || value > 0x7e {
			return nil, fmt.Errorf("local account password must contain printable ASCII characters only")
		}
	}
	saltedPassword := DeriveKey(password, salt, iterations)
	clientKey := hmacBytes(saltedPassword, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)
	signature := authSignature(storedKey[:], nonce, uin)
	proof := make([]byte, KeySize)
	for index := range proof {
		proof[index] = clientKey[index] ^ signature[index]
	}
	clear(saltedPassword)
	clear(clientKey)
	clear(signature)
	return proof, nil
}

func authSignature(storedKey, nonce []byte, uin uint32) []byte {
	mac := hmac.New(sha256.New, storedKey)
	mac.Write([]byte("QQTang local account login\x00"))
	var encodedUIN [4]byte
	binary.BigEndian.PutUint32(encodedUIN[:], uin)
	mac.Write(encodedUIN[:])
	mac.Write(nonce)
	return mac.Sum(nil)
}

func VerifyProof(verifier Verifier, nonce []byte, uin uint32, proof []byte) bool {
	if verifier.Validate() != nil || len(nonce) != NonceSize || len(proof) != sha256.Size {
		return false
	}
	signature := authSignature(verifier.StoredKey, nonce, uin)
	clientKey := make([]byte, KeySize)
	for index := range clientKey {
		clientKey[index] = proof[index] ^ signature[index]
	}
	gotStoredKey := sha256.Sum256(clientKey)
	accepted := subtle.ConstantTimeCompare(gotStoredKey[:], verifier.StoredKey) == 1
	clear(signature)
	clear(clientKey)
	return accepted
}

func hmacBytes(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}
