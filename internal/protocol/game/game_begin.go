package game

import (
	"fmt"
	"io"
)

const (
	// QQTMsgData.bin names transport command 0x0089 ID_SMC_NOTIFYGAMESTART.
	// The shipped NetCenter selects GAME_BEGIN_DATA schema 0x041C directly and
	// then recursively selects NOTIFY_GAME_BEGIN 0x0FA1. Therefore its payload
	// starts at GAME_BEGIN_DATA's uint16 length; it does not use the separate
	// 0x0084 / 0x041B NOTIFY_GAME_EVENT envelope.
	// QQTMsgData.bin's enum table stores MAX_SALE_ITEM_ONE_REPLAY=0x001E
	// immediately before this entry. The actual ID_NOTIFY_GAME_BEGIN value is
	// 0x0FA1; using the neighbouring limit constant makes the game manager
	// accept the outer event but dispatch the inner payload to the wrong case.
	NotifyGameBeginID = 0x0FA1

	DefaultAdventureGameTimeMS = 10 * 60 * 1000
	// DefaultCompetitiveGameTimeMS preserves the legacy GAME_BEGIN scalar.
	// Client.exe renders competitive countdowns from the active native rule
	// object's vtable instead, so this field must not be reused as the server's
	// authoritative per-rule deadline.
	DefaultCompetitiveGameTimeMS = 4 * 60 * 1000
)

// BuildLocalGameBegin encodes the shared NOTIFY_GAME_BEGIN transport used by
// every game category. Historical Adventure-named wrappers remain below for
// compatibility with existing probes and tests.
func BuildLocalGameBegin(requestPacket []byte, data GameBeginData) ([]byte, error) {
	return buildLocalAdventureGameBegin(requestPacket, data, nil)
}

// BuildLocalAdventureGameBegin sends the selected, resource-validated map
// rather than selecting a map in the transport layer.
func BuildLocalAdventureGameBegin(requestPacket []byte, data GameBeginData) ([]byte, error) {
	return buildLocalAdventureGameBegin(requestPacket, data, nil)
}

func BuildLocalAdventureGameBeginWithReader(requestPacket []byte, data GameBeginData, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalAdventureGameBegin(requestPacket, data, entropy)
}

func buildLocalAdventureGameBegin(requestPacket []byte, data GameBeginData, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != StartGameCommand {
		return nil, fmt.Errorf("game-begin trigger command 0x%04X, want 0x%04X", request.Command, StartGameCommand)
	}
	payload, err := data.MarshalLengthPrefixedNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalMessageFromRequest(requestPacket, request, GameBeginNotifyCommand, payload, entropy)
}
