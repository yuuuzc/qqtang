package qbv

import (
	"encoding/binary"
	"fmt"

	"qqtang/internal/protocol/game"
)

// ParseBootstrapGameBegin returns the exact 0x0FA1 payload saved by the
// original client before timed replay events. Unlike the QBV container, this
// payload is the normal big-endian GAME_BEGIN network structure.
func ParseBootstrapGameBegin(recording Recording) (game.GameBeginData, error) {
	record, err := uniqueBootstrap(recording, game.NotifyGameBeginID)
	if err != nil {
		return game.GameBeginData{}, err
	}
	result, err := game.ParseGameBeginDataNetwork(record.Data)
	if err != nil && len(record.Data) >= 4 && binary.BigEndian.Uint32(record.Data[len(record.Data)-4:]) == 0 {
		// Tencent's archived competitive recordings legitimately store a zero
		// GameTime in the bootstrap even though the live server contract used by
		// this project requires a non-zero duration. Re-parse the otherwise exact
		// payload with a harmless sentinel, then restore the recorded value. This
		// keeps the live protocol validator strict while allowing forensic replay
		// of the original files.
		archived := append([]byte(nil), record.Data...)
		binary.BigEndian.PutUint32(archived[len(archived)-4:], 1)
		result, err = game.ParseGameBeginDataNetwork(archived)
		result.GameTimeMS = 0
	}
	if err != nil {
		return game.GameBeginData{}, fmt.Errorf("parse QBV GAME_BEGIN bootstrap: %w", err)
	}
	if len(result.Players) != int(recording.Header.PlayerCount) {
		return game.GameBeginData{}, fmt.Errorf("QBV GAME_BEGIN has %d players, container declares %d", len(result.Players), recording.Header.PlayerCount)
	}
	return result, nil
}

// ParseBootstrapGameOver returns the result object recorded next to
// GAME_BEGIN. It is useful as the native terminal reference even when the
// timed event stream ends before the result UI finishes playing.
func ParseBootstrapGameOver(recording Recording) (game.GameOverData, error) {
	record, err := uniqueBootstrap(recording, game.NotifyGameOverEvent)
	if err != nil {
		return game.GameOverData{}, err
	}
	result, err := game.ParseGameOverData(record.Data)
	if err != nil {
		return game.GameOverData{}, fmt.Errorf("parse QBV GAME_OVER bootstrap: %w", err)
	}
	return result, nil
}

func uniqueBootstrap(recording Recording, schema uint16) (BootstrapRecord, error) {
	var found *BootstrapRecord
	for index := range recording.Bootstrap {
		record := &recording.Bootstrap[index]
		if record.Schema != uint32(schema) {
			continue
		}
		if found != nil {
			return BootstrapRecord{}, fmt.Errorf("QBV contains more than one bootstrap schema 0x%04X", schema)
		}
		found = record
	}
	if found == nil {
		return BootstrapRecord{}, fmt.Errorf("QBV contains no bootstrap schema 0x%04X", schema)
	}
	return *found, nil
}
