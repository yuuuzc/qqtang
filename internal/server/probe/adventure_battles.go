package probe

import (
	"fmt"

	"qqtang/internal/game/match"
)

// replaceAdventureBattle publishes match state at server scope rather than on
// one TCP connection. Future room members can therefore share a GameID and the
// same elimination state. The current local room contributes one participant.
func (server *Server) replaceAdventureBattle(gameID, mapID uint32, arbitratorID uint16, participants []match.AdventureParticipant) error {
	battle, err := match.NewAdventureBattle(gameID, mapID, arbitratorID, participants)
	if err != nil {
		return err
	}
	server.battleMu.Lock()
	defer server.battleMu.Unlock()
	if server.battles == nil {
		server.battles = make(map[uint32]*match.AdventureBattle)
	}
	server.battles[gameID] = battle
	return nil
}

func (server *Server) replaceAdventureBattleWithStage(gameID, mapID, itemSeed, expectedNPCDeaths uint32, arbitratorID uint16, participants []match.AdventureParticipant) error {
	battle, err := match.NewAdventureBattleWithStage(gameID, mapID, itemSeed, expectedNPCDeaths, arbitratorID, participants)
	if err != nil {
		return err
	}
	server.battleMu.Lock()
	defer server.battleMu.Unlock()
	if server.battles == nil {
		server.battles = make(map[uint32]*match.AdventureBattle)
	}
	server.battles[gameID] = battle
	return nil
}

func (server *Server) removeAdventureBattle(gameID uint32) {
	server.battleMu.Lock()
	delete(server.battles, gameID)
	server.battleMu.Unlock()
}

func (server *Server) adventureBattle(gameID uint32) (*match.AdventureBattle, error) {
	if gameID == 0 {
		return nil, fmt.Errorf("adventure game ID is zero")
	}
	server.battleMu.RLock()
	battle := server.battles[gameID]
	server.battleMu.RUnlock()
	if battle == nil {
		return nil, fmt.Errorf("adventure battle %d is not active", gameID)
	}
	return battle, nil
}
