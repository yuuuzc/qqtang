package application

import (
	"fmt"
	"sort"
	"time"

	roomstate "qqtang/internal/game/room"
)

type ChatScope byte

const (
	ChatScopeSection ChatScope = iota + 1
	ChatScopeRoom
	ChatScopeMatch
)

type ChatDelivery struct {
	Scope      ChatScope
	SectionID  uint16
	RoomID     uint16
	SenderUIN  uint32
	Text       string
	Time       time.Time
	Recipients []uint32
}

func (world *World) RouteSectionChat(uin uint32, sectionID uint16, text string, now time.Time) (ChatDelivery, error) {
	if world == nil || uin == 0 || sectionID == 0 || text == "" {
		return ChatDelivery{}, fmt.Errorf("section chat requires a world, sender, section, and text")
	}
	section, err := world.Section(sectionID)
	if err != nil {
		return ChatDelivery{}, err
	}
	message, err := section.AppendChat(uin, text, now)
	if err != nil {
		return ChatDelivery{}, err
	}
	snapshot := section.Snapshot(message.Sequence)
	recipients := make([]uint32, 0, len(snapshot.Players))
	for _, player := range snapshot.Players {
		recipients = append(recipients, player.UIN)
	}
	sort.Slice(recipients, func(i, j int) bool { return recipients[i] < recipients[j] })
	return ChatDelivery{
		Scope: ChatScopeSection, SectionID: sectionID, SenderUIN: uin,
		Text: text, Time: message.Time, Recipients: recipients,
	}, nil
}

func (world *World) RouteRoomChat(uin uint32, text string, now time.Time, matchOnly bool) (ChatDelivery, error) {
	if world == nil || uin == 0 || text == "" {
		return ChatDelivery{}, fmt.Errorf("room chat requires a world, sender, and text")
	}
	world.mu.RLock()
	membership, record, err := world.memberRoomLocked(uin)
	if err != nil {
		world.mu.RUnlock()
		return ChatDelivery{}, err
	}
	snapshot := record.state.Snapshot()
	if matchOnly && snapshot.Phase != roomstate.PhaseInMatch {
		world.mu.RUnlock()
		return ChatDelivery{}, fmt.Errorf("room %d is not in a match", membership.roomID)
	}
	recipients := make([]uint32, 0, len(snapshot.Members))
	for memberUIN, candidate := range world.memberships {
		if candidate.roomID == membership.roomID {
			recipients = append(recipients, memberUIN)
		}
	}
	world.mu.RUnlock()
	sort.Slice(recipients, func(i, j int) bool { return recipients[i] < recipients[j] })
	scope := ChatScopeRoom
	if matchOnly {
		scope = ChatScopeMatch
	}
	return ChatDelivery{
		Scope: scope, SectionID: membership.sectionID, RoomID: membership.roomID,
		SenderUIN: uin, Text: text, Time: now.UTC(), Recipients: recipients,
	}, nil
}
