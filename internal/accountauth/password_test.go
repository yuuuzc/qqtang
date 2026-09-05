package accountauth

import (
	"bytes"
	"testing"
)

func TestPerAccountSaltAndChallengeProof(t *testing.T) {
	first, err := NewVerifierWithReader(DefaultPassword, bytes.NewReader(bytes.Repeat([]byte{0x11}, SaltSize)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewVerifierWithReader(DefaultPassword, bytes.NewReader(bytes.Repeat([]byte{0x22}, SaltSize)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Salt, second.Salt) || bytes.Equal(first.StoredKey, second.StoredKey) {
		t.Fatal("different account salts produced identical stored credentials")
	}
	nonce := bytes.Repeat([]byte{0x33}, NonceSize)
	proof, err := ClientProof([]byte(DefaultPassword), first.Salt, first.Iterations, nonce, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyProof(first, nonce, 1_000_001, proof) {
		t.Fatal("valid proof was rejected")
	}
	if VerifyProof(first, nonce, 1_000_002, proof) || VerifyProof(second, nonce, 1_000_001, proof) {
		t.Fatal("proof was accepted for another account or verifier")
	}
	// A stolen database verifier is deliberately not a bearer login key. Even
	// though an attacker can calculate the server signature from StoredKey,
	// substituting that value for the unknown ClientKey cannot form a proof.
	forged := authSignature(first.StoredKey, nonce, 1_000_001)
	if VerifyProof(first, nonce, 1_000_001, forged) {
		t.Fatal("stored verifier could be used directly as a bearer login proof")
	}
}

func TestPasswordValidationMatchesLegacyInputLimit(t *testing.T) {
	for _, password := range []string{"short", "contains space", "12345678901234567", "密码123456"} {
		if ValidatePassword(password) == nil {
			t.Fatalf("password %q unexpectedly accepted", password)
		}
	}
	if err := ValidatePassword(DefaultPassword); err != nil {
		t.Fatal(err)
	}
}
