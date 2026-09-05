package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qqtang/internal/accountauth"
	"qqtang/internal/tooling/winlaunch"
)

func TestAuthenticateLocalAccountChallengeResponse(t *testing.T) {
	for _, test := range []struct {
		name     string
		password string
		wantOK   bool
	}{
		{name: "correct", password: accountauth.DefaultPassword, wantOK: true},
		{name: "wrong", password: "654321", wantOK: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			verifier, err := accountauth.NewVerifier(accountauth.DefaultPassword)
			if err != nil {
				t.Fatal(err)
			}
			nonce := bytes.Repeat([]byte{0x71}, accountauth.NonceSize)
			serverErr := make(chan error, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					serverErr <- acceptErr
					return
				}
				defer connection.Close()
				hello, readErr := readAuthMessage(connection)
				if readErr != nil || hello.Type != accountauth.MessageHello {
					serverErr <- readErr
					return
				}
				challenge, marshalErr := accountauth.Marshal(accountauth.Message{Type: accountauth.MessageChallenge, UIN: hello.UIN, Iterations: verifier.Iterations, Salt: verifier.Salt, Nonce: nonce})
				if marshalErr != nil {
					serverErr <- marshalErr
					return
				}
				if _, writeErr := connection.Write(challenge); writeErr != nil {
					serverErr <- writeErr
					return
				}
				proof, readErr := readAuthMessage(connection)
				if readErr != nil {
					serverErr <- readErr
					return
				}
				accepted := accountauth.VerifyProof(verifier, nonce, hello.UIN, proof.Proof)
				message := accountauth.Message{Type: accountauth.MessageResult, Accepted: accepted, Reason: "账号或密码错误"}
				if accepted {
					message.Nickname = "权威昵称"
					message.Gender = 1
				}
				result, marshalErr := accountauth.Marshal(message)
				if marshalErr == nil {
					_, marshalErr = connection.Write(result)
				}
				serverErr <- marshalErr
			}()
			started := time.Now()
			profile, authErr := authenticateLocalAccount(listener.Addr().String(), winlaunch.LocalLoginCredential{UIN: 1_000_001, Password: []byte(test.password)})
			elapsed := time.Since(started)
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
			if (authErr == nil) != test.wantOK {
				t.Fatalf("authenticate error = %v, wantOK=%v", authErr, test.wantOK)
			}
			if test.wantOK && (profile.Nickname != "权威昵称" || profile.Gender != 1) {
				t.Fatalf("authenticated profile = %+v", profile)
			}
			if !test.wantOK && elapsed >= 5*time.Second {
				t.Fatalf("wrong password result took %s; rejection must not use the legacy 30-second timeout", elapsed)
			}
		})
	}
}

func TestLoadFailurePayloadSelectsLastComplete1980Call(t *testing.T) {
	trace := winlaunch.SSOMessageCapture{Calls: []winlaunch.SSOMessageCall{
		{CopyDataID: 0x1980, DataLength: 3, CapturedLength: 2, PayloadHex: "0102"},
		{CopyDataID: 0x2000, DataLength: 2, CapturedLength: 2, PayloadHex: "aabb"},
		{CopyDataID: 0x1980, DataLength: 2, CapturedLength: 2, PayloadHex: "1020"},
		{CopyDataID: 0x1980, DataLength: 3, CapturedLength: 3, PayloadHex: "304050"},
	}}
	path := filepath.Join(t.TempDir(), "trace.json")
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	id, payload, err := loadFailurePayload(path)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0x1980 || string(payload) != string([]byte{0x30, 0x40, 0x50}) {
		t.Fatalf("id=0x%X payload=%X", id, payload)
	}
}

func TestLoadFailurePayloadRejectsIncompleteTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	if err := os.WriteFile(path, []byte(`{"calls":[{"copy_data_id":6528,"data_length":3,"captured_length":2,"payload_hex":"0102"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFailurePayload(path); err == nil {
		t.Fatal("expected incomplete-trace error")
	}
}

func TestStableProjectSSOReplayPayload(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "sso-message-trace-stable.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("stable project SSO payload is not present")
	}
	copyDataID, payload, err := loadFailurePayload(path)
	if err != nil {
		t.Fatal(err)
	}
	if copyDataID != 0x1980 || len(payload) != 571 {
		t.Fatalf("stable SSO payload = id 0x%X, length %d", copyDataID, len(payload))
	}
}
