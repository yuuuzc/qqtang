package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const gameMoneyNotificationSize = 11

// GameMoneyNotification mirrors NOTIFY_GAME_MONEY (schema 0x041F). Money is
// the new absolute account balance, not a delta. MoneyType is a legacy reason
// discriminator whose enumeration is not yet proven, so it remains numeric.
type GameMoneyNotification struct {
	ResultID  uint16
	MoneyType byte
	Money     uint32
	UIN       uint32
}

func (notification GameMoneyNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.UIN == 0 {
		return nil, fmt.Errorf("NOTIFY_GAME_MONEY UIN must be non-zero")
	}
	if notification.Money > MaxGameMoney {
		return nil, fmt.Errorf("NOTIFY_GAME_MONEY balance %d exceeds client maximum %d", notification.Money, MaxGameMoney)
	}
	payload := make([]byte, gameMoneyNotificationSize)
	binary.BigEndian.PutUint16(payload[0:2], notification.ResultID)
	payload[2] = notification.MoneyType
	binary.BigEndian.PutUint32(payload[3:7], notification.Money)
	binary.BigEndian.PutUint32(payload[7:11], notification.UIN)
	return payload, nil
}

func ParseGameMoneyNotification(payload []byte) (GameMoneyNotification, error) {
	if len(payload) != gameMoneyNotificationSize {
		return GameMoneyNotification{}, fmt.Errorf("NOTIFY_GAME_MONEY payload length %d, want %d", len(payload), gameMoneyNotificationSize)
	}
	result := GameMoneyNotification{
		ResultID: binary.BigEndian.Uint16(payload[0:2]), MoneyType: payload[2],
		Money: binary.BigEndian.Uint32(payload[3:7]), UIN: binary.BigEndian.Uint32(payload[7:11]),
	}
	if result.UIN == 0 || result.Money > MaxGameMoney {
		return GameMoneyNotification{}, fmt.Errorf("invalid NOTIFY_GAME_MONEY UIN/balance %d/%d", result.UIN, result.Money)
	}
	return result, nil
}

func decodeLocalGameMoneyRequest(packet []byte) (uint32, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return 0, err
	}
	if request.Command != GameMoneyCommand {
		return 0, fmt.Errorf("game-money command 0x%04X, want 0x%04X", request.Command, GameMoneyCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != 8 {
		return 0, fmt.Errorf("game-money payload length %d, want 8", len(payload))
	}
	uin := binary.BigEndian.Uint32(payload[0:4])
	if uin == 0 || uin != request.EnvelopeUIN {
		return 0, fmt.Errorf("game-money envelope UIN %d != payload UIN %d", request.EnvelopeUIN, uin)
	}
	return uin, nil
}

func BuildLocalGameMoneyResponse(requestPacket []byte, money uint32) ([]byte, error) {
	return buildLocalGameMoneyResponse(requestPacket, money, nil)
}

func buildLocalGameMoneyResponse(requestPacket []byte, money uint32, entropy io.Reader) ([]byte, error) {
	uin, err := decodeLocalGameMoneyRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint16(nil, 0)
	payload = binary.BigEndian.AppendUint32(payload, money)
	payload = binary.BigEndian.AppendUint32(payload, uin)
	return buildLocalResponse(requestPacket, request, GameMoneyCommand, payload, entropy)
}

// BuildLocalGameMoneyNotificationFromRequest creates the independent 0x0094
// push used after a reward changes sugar currency. It intentionally does not
// reuse the 0x008B get-balance request/response command.
func BuildLocalGameMoneyNotificationFromRequest(requestPacket []byte, notification GameMoneyNotification) ([]byte, error) {
	return buildLocalGameMoneyNotificationFromRequest(requestPacket, notification, nil)
}

func BuildLocalGameMoneyNotificationFromRequestWithReader(requestPacket []byte, notification GameMoneyNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalGameMoneyNotificationFromRequest(requestPacket, notification, entropy)
}

func buildLocalGameMoneyNotificationFromRequest(requestPacket []byte, notification GameMoneyNotification, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.EnvelopeUIN == 0 || notification.UIN != request.EnvelopeUIN {
		return nil, fmt.Errorf("NOTIFY_GAME_MONEY UIN %d does not match envelope UIN %d", notification.UIN, request.EnvelopeUIN)
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket, request, GameMoneyNotifyCommand, payload,
		localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, entropy,
	)
}
