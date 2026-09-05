package probe

func (server *Server) livePrimaryUINs() map[uint32]struct{} {
	server.liveMu.RLock()
	result := make(map[uint32]struct{}, len(server.liveSessions))
	for _, session := range server.liveSessions {
		if session == nil {
			continue
		}
		if uin := session.liveUIN.Load(); uin != 0 {
			result[uin] = struct{}{}
		}
	}
	server.liveMu.RUnlock()
	return result
}

// accountOnlineForGM treats both an owning game connection and the narrow
// authenticated reconnect lease as online. Deleting during either state can
// leave a running legacy client with durable state that no longer exists.
func (server *Server) accountOnlineForGM(uin uint32) bool {
	if uin == 0 {
		return false
	}
	server.liveMu.RLock()
	for _, session := range server.liveSessions {
		if session != nil && session.liveUIN.Load() == uin {
			server.liveMu.RUnlock()
			return true
		}
	}
	server.liveMu.RUnlock()
	return server.hasAuthenticatedSession(uin)
}
