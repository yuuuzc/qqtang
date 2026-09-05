package accountauth

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

const (
	MessageHello     byte = 1
	MessageChallenge byte = 2
	MessageProof     byte = 3
	MessageResult    byte = 4
)

var protocolMagic = [8]byte{'Q', 'Q', 'T', 'A', 'U', 'T', 'H', '1'}

type Message struct {
	Type       byte
	UIN        uint32
	Iterations uint32
	Salt       []byte
	Nonce      []byte
	Proof      []byte
	Accepted   bool
	Reason     string
	Nickname   string
	Gender     byte
}

func IsProtocolFrame(frame []byte) bool {
	return len(frame) >= 13 && string(frame[4:12]) == string(protocolMagic[:])
}

func Marshal(message Message) ([]byte, error) {
	payload := append([]byte(nil), protocolMagic[:]...)
	payload = append(payload, message.Type)
	switch message.Type {
	case MessageHello:
		if message.UIN == 0 {
			return nil, fmt.Errorf("account-auth hello requires a non-zero UIN")
		}
		payload = binary.BigEndian.AppendUint32(payload, message.UIN)
	case MessageChallenge:
		if message.UIN == 0 || message.Iterations == 0 || len(message.Salt) > 255 || len(message.Nonce) != NonceSize {
			return nil, fmt.Errorf("account-auth challenge is invalid")
		}
		payload = binary.BigEndian.AppendUint32(payload, message.UIN)
		payload = binary.BigEndian.AppendUint32(payload, message.Iterations)
		payload = append(payload, byte(len(message.Salt)))
		payload = append(payload, message.Salt...)
		payload = append(payload, message.Nonce...)
	case MessageProof:
		if message.UIN == 0 || len(message.Proof) != KeySize {
			return nil, fmt.Errorf("account-auth proof is invalid")
		}
		payload = binary.BigEndian.AppendUint32(payload, message.UIN)
		payload = append(payload, message.Proof...)
	case MessageResult:
		if len(message.Reason) > 200 {
			return nil, fmt.Errorf("account-auth result reason is too long")
		}
		if message.Accepted {
			payload = append(payload, 1)
		} else {
			payload = append(payload, 0)
		}
		payload = append(payload, byte(len(message.Reason)))
		payload = append(payload, message.Reason...)
		if message.Nickname != "" {
			if !message.Accepted {
				return nil, fmt.Errorf("rejected account-auth result cannot contain a profile")
			}
			if message.Gender > 1 {
				return nil, fmt.Errorf("account-auth result gender %d is invalid", message.Gender)
			}
			if !utf8.ValidString(message.Nickname) || len(message.Nickname) > 255 {
				return nil, fmt.Errorf("account-auth result nickname is invalid")
			}
			payload = append(payload, message.Gender, byte(len(message.Nickname)))
			payload = append(payload, message.Nickname...)
		}
	default:
		return nil, fmt.Errorf("unknown account-auth message type %d", message.Type)
	}
	frame := make([]byte, 4, 4+len(payload))
	frame = append(frame, payload...)
	binary.BigEndian.PutUint32(frame[:4], uint32(len(frame)))
	return frame, nil
}

func Unmarshal(frame []byte) (Message, error) {
	if len(frame) < 13 || int(binary.BigEndian.Uint32(frame[:4])) != len(frame) || !IsProtocolFrame(frame) {
		return Message{}, fmt.Errorf("invalid account-auth frame")
	}
	message := Message{Type: frame[12]}
	payload := frame[13:]
	switch message.Type {
	case MessageHello:
		if len(payload) != 4 {
			return Message{}, fmt.Errorf("account-auth hello payload length %d", len(payload))
		}
		message.UIN = binary.BigEndian.Uint32(payload)
	case MessageChallenge:
		if len(payload) < 9+NonceSize {
			return Message{}, fmt.Errorf("account-auth challenge payload length %d", len(payload))
		}
		message.UIN = binary.BigEndian.Uint32(payload[:4])
		message.Iterations = binary.BigEndian.Uint32(payload[4:8])
		saltLength := int(payload[8])
		if len(payload) != 9+saltLength+NonceSize {
			return Message{}, fmt.Errorf("account-auth challenge salt length %d", saltLength)
		}
		message.Salt = append([]byte(nil), payload[9:9+saltLength]...)
		message.Nonce = append([]byte(nil), payload[9+saltLength:]...)
	case MessageProof:
		if len(payload) != 4+KeySize {
			return Message{}, fmt.Errorf("account-auth proof payload length %d", len(payload))
		}
		message.UIN = binary.BigEndian.Uint32(payload[:4])
		message.Proof = append([]byte(nil), payload[4:]...)
	case MessageResult:
		if len(payload) < 2 {
			return Message{}, fmt.Errorf("account-auth result payload length %d", len(payload))
		}
		reasonEnd := 2 + int(payload[1])
		if len(payload) < reasonEnd {
			return Message{}, fmt.Errorf("account-auth result payload length %d", len(payload))
		}
		message.Accepted = payload[0] == 1
		message.Reason = string(payload[2:reasonEnd])
		profile := payload[reasonEnd:]
		if len(profile) != 0 {
			if !message.Accepted || len(profile) < 2 || len(profile) != 2+int(profile[1]) || profile[0] > 1 {
				return Message{}, fmt.Errorf("account-auth result profile length %d", len(profile))
			}
			message.Gender = profile[0]
			message.Nickname = string(profile[2:])
			if message.Nickname == "" || !utf8.ValidString(message.Nickname) {
				return Message{}, fmt.Errorf("account-auth result profile nickname is invalid")
			}
		}
	default:
		return Message{}, fmt.Errorf("unknown account-auth message type %d", message.Type)
	}
	return message, nil
}
