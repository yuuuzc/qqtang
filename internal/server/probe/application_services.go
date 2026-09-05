package probe

import (
	"fmt"

	"qqtang/internal/server/application"
)

// players returns the client-independent player application service. New
// wires it eagerly in production; the lazy path keeps focused package tests
// that construct Server literals small and backwards compatible.
func (server *Server) players() (*application.PlayerService, error) {
	if server == nil || server.playerStore == nil {
		return nil, fmt.Errorf("player store is unavailable")
	}
	if server.playerService != nil {
		return server.playerService, nil
	}
	server.applicationMu.Lock()
	defer server.applicationMu.Unlock()
	if server.playerService != nil {
		return server.playerService, nil
	}
	service, err := application.NewPlayerService(server.playerStore, server.equipmentCatalog)
	if err != nil {
		return nil, err
	}
	server.playerService = service
	return service, nil
}

// worldState returns the single authoritative online-state application
// service. Production constructs it eagerly; the guarded lazy path keeps
// focused adapter tests concise without reintroducing probe-owned maps.
func (server *Server) worldState() *application.World {
	if server == nil {
		return nil
	}
	if server.world != nil {
		return server.world
	}
	server.applicationMu.Lock()
	defer server.applicationMu.Unlock()
	if server.world == nil {
		server.world = application.NewWorld()
	}
	return server.world
}
