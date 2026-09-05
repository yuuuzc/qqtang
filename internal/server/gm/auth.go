package gm

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	authSchemaVersion = 1
	defaultIterations = 210_000
	derivedKeyBytes   = 32
)

// Credentials is a salted password verifier. The GM password itself is never
// written to disk. HTTP Basic authentication still requires TLS when the GM
// port is exposed beyond a trusted LAN.
type Credentials struct {
	SchemaVersion int    `json:"schema_version"`
	Username      string `json:"username"`
	Iterations    int    `json:"iterations"`
	Salt          string `json:"salt"`
	PasswordHash  string `json:"password_hash"`
}

func NewCredentials(username, password string) (*Credentials, error) {
	username = strings.TrimSpace(username)
	if err := validateUsernamePassword(username, password); err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate GM password salt: %w", err)
	}
	result := &Credentials{
		SchemaVersion: authSchemaVersion,
		Username:      username,
		Iterations:    defaultIterations,
		Salt:          base64.StdEncoding.EncodeToString(salt),
	}
	result.PasswordHash = base64.StdEncoding.EncodeToString(deriveKey([]byte(password), salt, result.Iterations, derivedKeyBytes))
	return result, nil
}

func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GM authentication config: %w", err)
	}
	var credentials Credentials
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil {
		return nil, fmt.Errorf("decode GM authentication config: %w", err)
	}
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return &credentials, nil
}

func SaveCredentials(path string, credentials *Credentials) error {
	if credentials == nil {
		return fmt.Errorf("GM credentials are nil")
	}
	if err := credentials.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("encode GM authentication config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create GM authentication config directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write GM authentication config: %w", err)
	}
	return nil
}

func (credentials *Credentials) Validate() error {
	if credentials == nil {
		return fmt.Errorf("GM credentials are nil")
	}
	if credentials.SchemaVersion != authSchemaVersion {
		return fmt.Errorf("GM authentication schema_version %d, want %d", credentials.SchemaVersion, authSchemaVersion)
	}
	if err := validateUsernamePassword(credentials.Username, "x"); err != nil {
		return err
	}
	if credentials.Iterations < 100_000 || credentials.Iterations > 2_000_000 {
		return fmt.Errorf("GM password iterations are outside the safe range")
	}
	salt, err := base64.StdEncoding.DecodeString(credentials.Salt)
	if err != nil || len(salt) < 16 {
		return fmt.Errorf("GM password salt is invalid")
	}
	hash, err := base64.StdEncoding.DecodeString(credentials.PasswordHash)
	if err != nil || len(hash) != derivedKeyBytes {
		return fmt.Errorf("GM password hash is invalid")
	}
	return nil
}

func (credentials *Credentials) Verify(username, password string) bool {
	if credentials == nil || credentials.Validate() != nil {
		return false
	}
	salt, _ := base64.StdEncoding.DecodeString(credentials.Salt)
	want, _ := base64.StdEncoding.DecodeString(credentials.PasswordHash)
	got := deriveKey([]byte(password), salt, credentials.Iterations, len(want))
	usernameOK := subtle.ConstantTimeCompare([]byte(username), []byte(credentials.Username))
	passwordOK := subtle.ConstantTimeCompare(got, want)
	return usernameOK&passwordOK == 1
}

func validateUsernamePassword(username, password string) error {
	if len(username) < 1 || len(username) > 64 || strings.ContainsAny(username, ":\r\n") {
		return fmt.Errorf("GM username must be 1-64 characters and cannot contain colon or newlines")
	}
	if len(password) < 1 || len(password) > 256 {
		return fmt.Errorf("GM password must be 1-256 characters")
	}
	return nil
}

func deriveKey(password, salt []byte, iterations, length int) []byte {
	blocks := (length + sha256.Size - 1) / sha256.Size
	result := make([]byte, 0, blocks*sha256.Size)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for round := 1; round < iterations; round++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for index := range t {
				t[index] ^= u[index]
			}
		}
		result = append(result, t...)
	}
	return result[:length]
}
