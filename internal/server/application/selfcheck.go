package application

import (
	"context"
	"fmt"
	"time"

	"qqtang/internal/accountauth"
	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/petcatalog"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/game/shopcatalog"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

// HeadlessCheckReport is emitted by the client-free server self-check. The
// scenario uses a caller-selected SQLite file and exercises durable player
// data plus the in-memory lobby/room/match lifecycle.
type HeadlessCheckReport struct {
	Database          string   `json:"database"`
	Checks            []string `json:"checks"`
	PurchasedItemID   uint16   `json:"purchased_item_id"`
	RemainingMoney    uint32   `json:"remaining_money"`
	AdventurePoints   uint32   `json:"adventure_points"`
	CreatedPetID      uint32   `json:"created_pet_id"`
	CompletedGameID   uint32   `json:"completed_game_id"`
	FinalLobbyPlayers int      `json:"final_lobby_players"`
}

// RunHeadlessCheck verifies mainstream application use cases without opening
// a network listener or launching Client.exe. It is intentionally built from
// the same PlayerService and World APIs used by protocol adapters.
func RunHeadlessCheck(ctx context.Context, databasePath string) (HeadlessCheckReport, error) {
	report := HeadlessCheckReport{Database: databasePath}
	if databasePath == "" {
		return report, fmt.Errorf("headless check requires a SQLite database path")
	}
	store, err := persistence.OpenPlayerStore(databasePath)
	if err != nil {
		return report, fmt.Errorf("open self-check player store: %w", err)
	}
	defer store.Close()
	players, err := NewPlayerService(store, nil)
	if err != nil {
		return report, err
	}

	const firstUIN uint32 = 9_900_001
	const secondUIN uint32 = 9_900_002
	first := game.DefaultPlayerProfile()
	first.Nickname = "自检一号"
	first.GameInfo.Money = 5_000
	first, err = players.LoadOrCreate(ctx, firstUIN, first)
	if err != nil {
		return report, fmt.Errorf("create first self-check account: %w", err)
	}
	second := game.DefaultPlayerProfile()
	second.PlayerID = 2
	second.Nickname = "自检二号"
	second, err = players.LoadOrCreate(ctx, secondUIN, second)
	if err != nil {
		return report, fmt.Errorf("create second self-check account: %w", err)
	}
	report.Checks = append(report.Checks, "account_persistence")
	iterations, salt, err := store.PasswordParameters(ctx, firstUIN)
	if err != nil {
		return report, fmt.Errorf("load self-check password parameters: %w", err)
	}
	nonce := make([]byte, accountauth.NonceSize)
	for index := range nonce {
		nonce[index] = byte(index + 1)
	}
	proof, err := accountauth.ClientProof([]byte(accountauth.DefaultPassword), salt, iterations, nonce, firstUIN)
	if err != nil {
		return report, fmt.Errorf("build self-check password proof: %w", err)
	}
	accepted, err := store.VerifyPasswordProof(ctx, firstUIN, nonce, proof)
	clear(proof)
	if err != nil {
		return report, fmt.Errorf("verify self-check password proof: %w", err)
	}
	if !accepted {
		return report, fmt.Errorf("self-check password proof was rejected")
	}
	report.Checks = append(report.Checks, "password_challenge_proof")

	catalog, err := shopcatalog.NewCatalog(848,
		[]itemcatalog.RegistryEntry{{ItemID: game.LargeStaminaPotionItemID, Category: "item", ResourceID: 1, Name: "大体力药水"}},
		[]itemcatalog.CommodityEntry{{CommodityID: 9_000_001, Category: "item", ResourceID: 1, Name: "大体力药水"}}, 10)
	if err != nil {
		return report, fmt.Errorf("build self-check shop catalog: %w", err)
	}
	purchase, err := players.PurchaseCommodity(ctx, firstUIN, catalog, 9_000_001, PaymentGameMoney, false)
	if err != nil {
		return report, fmt.Errorf("purchase self-check item: %w", err)
	}
	if purchase.Status != PurchaseSuccess {
		return report, fmt.Errorf("self-check purchase status %q: %s", purchase.Status, purchase.Message)
	}
	first, err = players.UpdateItemStatuses(ctx, firstUIN, purchase.Profile.GameInfo.RoleID, purchase.Profile,
		[]game.ItemStatusChange{{ItemID: uint32(game.LargeStaminaPotionItemID), NewStatus: 1}})
	if err != nil {
		return report, fmt.Errorf("prepare self-check item: %w", err)
	}
	first, _, err = players.ConsumePreparedItem(ctx, firstUIN, first.GameInfo.RoleID, first, game.LargeStaminaPotionItemID)
	if err != nil {
		return report, fmt.Errorf("consume self-check item: %w", err)
	}
	report.PurchasedItemID = game.LargeStaminaPotionItemID
	report.RemainingMoney = first.GameInfo.Money
	report.Checks = append(report.Checks, "shop_inventory_item_use")

	if err = store.SetInventoryItem(ctx, firstUIN, game.NewPermanentItemInfo(28_036, 1)); err != nil {
		return report, fmt.Errorf("seed self-check pet card: %w", err)
	}
	if err = store.SetInventoryItem(ctx, firstUIN, game.NewPermanentItemInfo(27_001, 1)); err != nil {
		return report, fmt.Errorf("seed self-check skill book: %w", err)
	}
	if err = store.SetInventoryItem(ctx, firstUIN, game.NewPermanentItemInfo(petcatalog.PetFoodLargeItemID, 1)); err != nil {
		return report, fmt.Errorf("seed self-check pet food: %w", err)
	}
	adopted, err := players.AdoptPetFromCard(ctx, firstUIN, petcatalog.CardLink{
		ItemID: 28_036, ResourceID: 36, PetTypeID: 25_024, Name: "旺旺狗",
	})
	if err != nil {
		return report, fmt.Errorf("adopt self-check pet: %w", err)
	}
	learned, err := players.LearnPetSkillFromBook(ctx, firstUIN, adopted.Pet.PetID, petcatalog.SkillBookLink{
		ItemID: 27_001, ResourceID: 1, Name: "糖币加加1级",
		Skill: petcatalog.SkillDefinition{SkillID: 51, Name: "糖币加加1级", Level: 1, Learned: true},
	})
	if err != nil || len(learned.Pet.Skills) != 1 || learned.Pet.Skills[0] != 51 {
		return report, fmt.Errorf("learn self-check pet skill: result=%+v err=%v", learned, err)
	}
	fed, err := players.FeedPet(ctx, firstUIN, learned.Pet.PetID, petcatalog.FoodLink{
		ItemID: uint32(petcatalog.PetFoodLargeItemID), Complete: true,
		Effect: petcatalog.FoodEffect{Loyalty: 1000, Experience: 60},
	}, []uint32{0, 50, 150})
	if err != nil || fed.RemainingQuantity != 0 || fed.Pet.PetExperience != 60 || fed.Pet.PetLevel != 2 {
		return report, fmt.Errorf("feed self-check pet: result=%+v err=%v", fed, err)
	}
	if _, err = players.SetPetActive(ctx, firstUIN, learned.Pet.PetID, true); err != nil {
		return report, fmt.Errorf("activate self-check pet: %w", err)
	}
	first, err = players.Load(ctx, firstUIN)
	if err != nil {
		return report, fmt.Errorf("reload self-check pet profile: %w", err)
	}
	report.CreatedPetID = learned.Pet.PetID
	report.Checks = append(report.Checks, "pet_card_food_skill_lifecycle")

	if err = store.SetInventoryItem(ctx, firstUIN, game.NewPermanentItemInfo(persistence.MarriageProposalItemID, 1)); err != nil {
		return report, fmt.Errorf("seed self-check proposal item: %w", err)
	}
	if _, err = store.CreateMarriageProposal(ctx, firstUIN, secondUIN, "自检求婚"); err != nil {
		return report, fmt.Errorf("create self-check marriage proposal: %w", err)
	}
	marriage, err := store.AnswerMarriageProposal(ctx, secondUIN, firstUIN, true)
	if err != nil {
		return report, fmt.Errorf("accept self-check marriage proposal: %w", err)
	}
	if marriage.UIN != secondUIN || marriage.SpouseUIN != firstUIN {
		return report, fmt.Errorf("self-check accepted marriage pair = %d/%d, want %d/%d",
			marriage.UIN, marriage.SpouseUIN, secondUIN, firstUIN)
	}
	first, err = players.Load(ctx, firstUIN)
	if err != nil {
		return report, fmt.Errorf("reload first self-check marriage profile: %w", err)
	}
	second, err = players.Load(ctx, secondUIN)
	if err != nil {
		return report, fmt.Errorf("reload second self-check marriage profile: %w", err)
	}
	if first.SpouseUIN != secondUIN || second.SpouseUIN != firstUIN {
		return report, fmt.Errorf("self-check marriage projection = %d/%d, want %d/%d",
			first.SpouseUIN, second.SpouseUIN, secondUIN, firstUIN)
	}
	if _, err = store.UpdateMarriageLoveWord(ctx, firstUIN, secondUIN, "自检爱情宣言"); err != nil {
		return report, fmt.Errorf("update self-check love word: %w", err)
	}
	if err = store.Divorce(ctx, firstUIN, secondUIN); err != nil {
		return report, fmt.Errorf("divorce self-check marriage: %w", err)
	}
	first, err = players.Load(ctx, firstUIN)
	if err != nil {
		return report, fmt.Errorf("reload divorced self-check profile: %w", err)
	}
	if first.SpouseUIN != 0 {
		return report, fmt.Errorf("self-check divorced spouse = %d, want 0", first.SpouseUIN)
	}
	report.Checks = append(report.Checks, "marriage_proposal_profile_divorce_lifecycle")

	if _, err = store.RequestFriend(ctx, firstUIN, secondUIN, "自检好友申请"); err != nil {
		return report, fmt.Errorf("create self-check friend request: %w", err)
	}
	changedOwners, err := store.AnswerFriendRequest(ctx, secondUIN, firstUIN, true)
	if err != nil || len(changedOwners) != 1 || changedOwners[0] != firstUIN {
		return report, fmt.Errorf("accept self-check friend request: owners=%v err=%v", changedOwners, err)
	}
	firstFriends, err := store.ListFriends(ctx, firstUIN)
	if err != nil || len(firstFriends) != 1 || firstFriends[0] != secondUIN {
		return report, fmt.Errorf("load first self-check friend list: friends=%v err=%v", firstFriends, err)
	}
	secondFriends, err := store.ListFriends(ctx, secondUIN)
	if err != nil || len(secondFriends) != 0 {
		return report, fmt.Errorf("load second self-check friend list: friends=%v err=%v", secondFriends, err)
	}
	if err = store.RemoveFriend(ctx, firstUIN, secondUIN); err != nil {
		return report, fmt.Errorf("remove first self-check friend: %w", err)
	}
	report.Checks = append(report.Checks, "directed_friend_request_accept_remove_lifecycle")

	first, progression, err := players.ApplyAdventureSettlement(ctx, firstUIN, first, AdventureSettlement{
		Result: game.GameResultWin, AdventurePoints: 110,
		CollectedItems: map[uint32]uint32{30_067: 3},
	})
	if err != nil {
		return report, fmt.Errorf("apply self-check settlement: %w", err)
	}
	report.AdventurePoints = progression.AppliedPoints
	report.Checks = append(report.Checks, "adventure_settlement")

	world := NewWorld()
	if err = world.EnterLobby(firstUIN, first); err != nil {
		return report, err
	}
	if err = world.EnterLobby(secondUIN, second); err != nil {
		return report, err
	}
	if delivery, routeErr := world.RouteSectionChat(firstUIN, first.SectionID, "自检大厅消息", time.Unix(1, 0)); routeErr != nil || len(delivery.Recipients) != 2 {
		return report, fmt.Errorf("route self-check section chat: delivery=%+v err=%v", delivery, routeErr)
	}
	created, err := world.CreateRoom(firstUIN, first, roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeAdventure,
	}, roomstate.Properties{Name: "无客户端自检房间"})
	if err != nil {
		return report, fmt.Errorf("create self-check room: %w", err)
	}
	if _, err = world.JoinRoom(secondUIN, second, created.RoomID, second.GameInfo.RoleID, 2); err != nil {
		return report, fmt.Errorf("join self-check room: %w", err)
	}
	if _, err = world.SetReady(secondUIN, true); err != nil {
		return report, fmt.Errorf("ready self-check player: %w", err)
	}
	if delivery, routeErr := world.RouteRoomChat(firstUIN, "自检房间消息", time.Unix(2, 0), false); routeErr != nil || len(delivery.Recipients) != 2 {
		return report, fmt.Errorf("route self-check room chat: delivery=%+v err=%v", delivery, routeErr)
	}
	_, gameID, err := world.StartMatch(firstUIN)
	if err != nil {
		return report, fmt.Errorf("start self-check match: %w", err)
	}
	if delivery, routeErr := world.RouteRoomChat(firstUIN, "自检对局消息", time.Unix(3, 0), true); routeErr != nil || len(delivery.Recipients) != 2 {
		return report, fmt.Errorf("route self-check match chat: delivery=%+v err=%v", delivery, routeErr)
	}
	if _, err = world.CompleteMatch(firstUIN, gameID); err != nil {
		return report, fmt.Errorf("complete self-check match: %w", err)
	}
	if _, err = world.LeaveRoom(firstUIN, roomstate.LeaveVoluntary); err != nil {
		return report, fmt.Errorf("leave self-check owner: %w", err)
	}
	if _, err = world.LeaveRoom(secondUIN, roomstate.LeaveVoluntary); err != nil {
		return report, fmt.Errorf("leave self-check final player: %w", err)
	}
	lobby, err := world.LobbySnapshot(first.SectionID, 0)
	if err != nil {
		return report, err
	}
	report.CompletedGameID = gameID
	report.FinalLobbyPlayers = len(lobby.Players)
	report.Checks = append(report.Checks, "lobby_room_match_chat_lifecycle")
	return report, nil
}
