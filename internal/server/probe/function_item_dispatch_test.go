package probe

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"qqtang/internal/game/functionitem"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestBreakEggHandlerCommitsOneAtomicExchangeBeforeResult(t *testing.T) {
	ctx := context.Background()
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		game.NewPermanentItemInfo(9001, 1),
		game.NewPermanentItemInfo(9011, 1),
		game.NewPermanentItemInfo(294, 2),
	}
	profile, err = store.LoadOrCreate(ctx, uin, profile)
	if err != nil {
		t.Fatal(err)
	}
	rewardPath := filepath.Join(t.TempDir(), "break-egg.json")
	rewardJSON := `{"schema_version":1,"egg_tiers":{"9001":1,"9002":2,"9003":3,"9004":4},"hammer_boost":{"9011":0,"9012":1,"9013":2},"reward_tiers":{"1":[{"item_id":294,"quantity":2,"weight":1}],"2":[{"item_id":294,"quantity":2,"weight":1}],"3":[{"item_id":294,"quantity":2,"weight":1}],"4":[{"item_id":294,"quantity":2,"weight":1}]}}`
	if err = os.WriteFile(rewardPath, []byte(rewardJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := functionitem.LoadBreakEggCatalog(rewardPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{playerStore: store, breakEggCatalog: catalog}
	session := &connectionSession{UIN: uin, Profile: profile, auxiliary: true}
	payload := make([]byte, 16)
	binary.BigEndian.PutUint32(payload[0:4], uin)
	binary.BigEndian.PutUint32(payload[4:8], 1234)
	binary.BigEndian.PutUint32(payload[8:12], 9011)
	binary.BigEndian.PutUint32(payload[12:16], 9001)
	// The original shop emits break-egg through the personal route (4/1).
	request := testLocalRoutedPacketWithPayload(t, game.BreakEggRequestCommand, 4, 0xffff, 1, uin, payload)
	requestInspection, err := game.InspectLocalPacket(request)
	if err != nil {
		t.Fatal(err)
	}
	if requestInspection.Command != 0x00FE || len(requestInspection.Payload) != 16 {
		t.Fatalf("real break-egg request shape = command 0x%04X payload %d", requestInspection.Command, len(requestInspection.Payload))
	}

	result := server.handleFunctionalItemMessage(session, "test", request)
	if !result.handled || len(result.response) == 0 || len(result.beforeResponse) == 0 || result.result != "break_egg_tier_1_reward_294_x2" {
		t.Fatalf("break-egg result = %+v", result)
	}
	response, err := game.InspectLocalPacket(result.response)
	if err != nil {
		t.Fatal(err)
	}
	if response.Command != game.BreakEggResponseCommand || response.Route != 4 || response.SectionID != 1 || binary.BigEndian.Uint16(response.Payload[0:2]) != 0 || binary.BigEndian.Uint32(response.Payload[2:6]) != 294 {
		t.Fatalf("break-egg response = command 0x%04X payload %x", response.Command, response.Payload)
	}
	refreshPacket, err := game.InspectLocalPacket(result.beforeResponse)
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := game.ParsePlayerItemAddNotification(refreshPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint16]uint32{294: 4, 9001: 0, 9011: 0}
	if len(refresh.Items) != len(want) {
		t.Fatalf("break-egg inventory refresh = %+v", refresh.Items)
	}
	for _, item := range refresh.Items {
		if item.NumOfItem != want[item.ItemID] {
			t.Fatalf("break-egg absolute item %+v, want %d", item, want[item.ItemID])
		}
	}
	for itemID, quantity := range want {
		item, found, loadErr := store.InventoryItem(ctx, uin, itemID)
		if loadErr != nil || quantity != 0 && (!found || item.NumOfItem != quantity) || quantity == 0 && found {
			t.Fatalf("persisted item %d = found:%t item:%+v err:%v", itemID, found, item, loadErr)
		}
	}
}
