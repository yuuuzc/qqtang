package accountauth

import (
	"bytes"
	"testing"
)

func TestProtocolRoundTrip(t *testing.T) {
	tests := []Message{
		{Type: MessageHello, UIN: 1_000_001},
		{Type: MessageChallenge, UIN: 1_000_001, Iterations: DefaultIterations, Salt: bytes.Repeat([]byte{1}, SaltSize), Nonce: bytes.Repeat([]byte{2}, NonceSize)},
		{Type: MessageProof, UIN: 1_000_001, Proof: bytes.Repeat([]byte{3}, KeySize)},
		{Type: MessageResult, Accepted: true, Reason: "ok", Nickname: "自定义角色", Gender: 1},
	}
	for _, test := range tests {
		encoded, err := Marshal(test)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Unmarshal(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Type != test.Type || decoded.UIN != test.UIN || decoded.Accepted != test.Accepted || decoded.Reason != test.Reason || decoded.Nickname != test.Nickname || decoded.Gender != test.Gender || !bytes.Equal(decoded.Salt, test.Salt) || !bytes.Equal(decoded.Nonce, test.Nonce) || !bytes.Equal(decoded.Proof, test.Proof) {
			t.Fatalf("round trip = %+v, want %+v", decoded, test)
		}
	}
}
