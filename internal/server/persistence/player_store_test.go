package persistence

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"qqtang/internal/accountauth"
	"qqtang/internal/game/craftcatalog"
	"qqtang/internal/game/equipment"
	"qqtang/internal/game/itemeffect"
	"qqtang/internal/game/petcatalog"
	"qqtang/internal/protocol/game"
)

// Historical migration fixtures are intentionally retained only as archived
// regression documentation. The supported player database is the final schema.
const currentPlayerStoreSchemaVersion = 0

func TestPlayerStoreUsesPerAccountSaltAndVerifiesChallengeProof(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, uin := range []uint32{1_000_001, 1_000_002} {
		if _, err := store.LoadOrCreate(ctx, uin, game.DefaultPlayerProfile()); err != nil {
			t.Fatal(err)
		}
	}
	iterations, firstSalt, err := store.PasswordParameters(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	_, secondSalt, err := store.PasswordParameters(ctx, 1_000_002)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstSalt, secondSalt) {
		t.Fatal("two local accounts share a password salt")
	}
	nonce := bytes.Repeat([]byte{0x45}, accountauth.NonceSize)
	defaultProof, err := accountauth.ClientProof([]byte(accountauth.DefaultPassword), firstSalt, iterations, nonce, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.VerifyPasswordProof(ctx, 1_000_001, nonce, defaultProof)
	clear(defaultProof)
	if err != nil || !accepted {
		t.Fatalf("default password proof accepted=%v, err=%v", accepted, err)
	}
	if err := store.SetPassword(ctx, 1_000_001, "newpass8"); err != nil {
		t.Fatal(err)
	}
	iterations, firstSalt, err = store.PasswordParameters(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	newProof, err := accountauth.ClientProof([]byte("newpass8"), firstSalt, iterations, nonce, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err = store.VerifyPasswordProof(ctx, 1_000_001, nonce, newProof)
	clear(newProof)
	if err != nil || !accepted {
		t.Fatalf("updated password proof accepted=%v, err=%v", accepted, err)
	}
}

func TestPlayerStorePersistsProfileByUIN(t *testing.T) {
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seed := game.DefaultPlayerProfile()
	seed.GameInfo.ExtPoint = 5_200_000
	first, err := store.LoadOrCreate(context.Background(), 1_000_001, seed)
	if err != nil {
		t.Fatal(err)
	}
	if first.GameInfo.ExtPoint != 5_200_000 {
		t.Fatalf("first adventure points = %d", first.GameInfo.ExtPoint)
	}
	first.GameInfo.ExtPoint = 5_300_000
	if err := store.Save(context.Background(), 1_000_001, first); err != nil {
		t.Fatal(err)
	}
	otherSeed := game.DefaultPlayerProfile()
	reloaded, err := store.LoadOrCreate(context.Background(), 1_000_001, otherSeed)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GameInfo.ExtPoint != 5_300_000 {
		t.Fatalf("reloaded adventure points = %d", reloaded.GameInfo.ExtPoint)
	}
}

func TestSaveProfilesRollsBackWholeRoomBatch(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const firstUIN uint32 = 1_000_001
	const secondUIN uint32 = 1_000_002
	first := game.DefaultPlayerProfile()
	first.GameInfo.Money = 10
	if err = store.Save(ctx, firstUIN, first); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(ctx, secondUIN, game.DefaultPlayerProfile()); err != nil {
		t.Fatal(err)
	}
	if err = store.SetInventoryItem(ctx, secondUIN, game.NewPermanentItemInfo(uint16(itemeffect.PetSlotExpansion10ItemID), 1)); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < game.PlayerPetsBaseCapacity+1; index++ {
		if _, err = store.GrantPet(ctx, secondUIN, uint32(26_000+index), "batch"); err != nil {
			t.Fatal(err)
		}
	}
	first, err = store.Load(ctx, firstUIN)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Load(ctx, secondUIN)
	if err != nil {
		t.Fatal(err)
	}
	first.GameInfo.Money = 99
	second.Inventory = nil // invalid only against the separately stored six pets
	err = store.SaveProfiles(ctx, map[uint32]game.PlayerProfile{firstUIN: first, secondUIN: second})
	if !errors.Is(err, ErrPetCapacityReduction) {
		t.Fatalf("room batch failure = %v", err)
	}
	reloadedFirst, err := store.Load(ctx, firstUIN)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedFirst.GameInfo.Money != 10 {
		t.Fatalf("earlier profile committed before later failure: money=%d", reloadedFirst.GameInfo.Money)
	}
	if _, found, err := store.InventoryItem(ctx, secondUIN, uint16(itemeffect.PetSlotExpansion10ItemID)); err != nil || !found {
		t.Fatalf("later profile inventory was partially replaced: found=%t err=%v", found, err)
	}
}

func TestMarriageLifecycleProjectsBothProfiles(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index, uin := range []uint32{1_000_001, 1_000_002} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		profile.Nickname = []string{"糖一", "糖二"}[index]
		if _, err := store.LoadOrCreate(ctx, uin, profile); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateMarriageProposal(ctx, 1_000_001, 1_000_002, "嫁给我吧"); !errors.Is(err, ErrMarriageProposalItemMissing) {
		t.Fatalf("proposal without item = %v", err)
	}
	proposer, err := store.Load(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	proposer.Inventory = append(proposer.Inventory, game.NewPermanentItemInfo(MarriageProposalItemID, 1))
	if err := store.Save(ctx, 1_000_001, proposer); err != nil {
		t.Fatal(err)
	}
	proposal, err := store.CreateMarriageProposal(ctx, 1_000_001, 1_000_002, "嫁给我吧")
	if err != nil || proposal.ProposerNickname != "糖一" || proposal.TargetNickname != "糖二" {
		t.Fatalf("proposal = %+v, err=%v", proposal, err)
	}
	marriage, err := store.AnswerMarriageProposal(ctx, 1_000_002, 1_000_001, true)
	if err != nil || marriage.SpouseUIN != 1_000_001 || marriage.LevelValue == 0 {
		t.Fatalf("marriage = %+v, err=%v", marriage, err)
	}
	for uin, spouse := range map[uint32]uint32{1_000_001: 1_000_002, 1_000_002: 1_000_001} {
		profile, err := store.Load(ctx, uin)
		if err != nil {
			t.Fatal(err)
		}
		if profile.SpouseUIN != spouse {
			t.Fatalf("UIN %d spouse projection = %d, want %d", uin, profile.SpouseUIN, spouse)
		}
	}
	proposer, err = store.Load(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	proposer.Inventory = append(proposer.Inventory,
		game.NewPermanentItemInfo(MarriageRingItemFirst, 1),
		game.NewPermanentItemInfo(MarriageRingItemFirst+2, 1),
	)
	if err := store.Save(ctx, 1_000_001, proposer); err != nil {
		t.Fatal(err)
	}
	marriage, err = store.SynchronizeMarriageRing(ctx, 1_000_002)
	if err != nil || marriage.RingID != uint32(MarriageRingItemFirst+2) {
		t.Fatalf("synchronized marriage ring = %+v, err=%v", marriage, err)
	}
	spouse, err := store.Load(ctx, 1_000_002)
	if err != nil {
		t.Fatal(err)
	}
	spouse.Inventory = append(spouse.Inventory,
		game.NewPermanentItemInfo(MarriageRingItemFirst+1, 1),
		game.NewPermanentItemInfo(MarriageRingItemLast, 1),
	)
	if err := store.Save(ctx, 1_000_002, spouse); err != nil {
		t.Fatal(err)
	}
	marriage, err = store.SynchronizeMarriageRing(ctx, 1_000_001)
	if err != nil || marriage.RingID != uint32(MarriageRingItemLast) {
		t.Fatalf("multi-ring marriage projection = %+v, err=%v", marriage, err)
	}
	if err := spouse.SetPermanentInventoryItem(MarriageRingItemLast, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, 1_000_002, spouse); err != nil {
		t.Fatal(err)
	}
	marriage, err = store.SynchronizeMarriageRing(ctx, 1_000_002)
	if err != nil || marriage.RingID != uint32(MarriageRingItemFirst+2) {
		t.Fatalf("multi-ring fallback projection = %+v, err=%v", marriage, err)
	}
	updated, err := store.UpdateMarriageLoveWord(ctx, 1_000_001, 1_000_002, "永远在一起")
	if err != nil || updated.LoveWord != "永远在一起" {
		t.Fatalf("updated marriage = %+v, err=%v", updated, err)
	}
	if err := store.Divorce(ctx, 1_000_002, 1_000_001); err != nil {
		t.Fatal(err)
	}
	for _, uin := range []uint32{1_000_001, 1_000_002} {
		profile, err := store.Load(ctx, uin)
		if err != nil {
			t.Fatal(err)
		}
		if profile.SpouseUIN != 0 {
			t.Fatalf("divorced UIN %d still projects spouse %d", uin, profile.SpouseUIN)
		}
	}
	if _, err := store.CreateMarriageProposal(ctx, 1_000_001, 1_000_002, "再试一次"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AnswerMarriageProposal(ctx, 1_000_002, 1_000_001, true); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Delete(ctx, 1_000_001)
	if err != nil || !deleted {
		t.Fatalf("delete married player = %v, %v", deleted, err)
	}
	survivor, err := store.Load(ctx, 1_000_002)
	if err != nil {
		t.Fatal(err)
	}
	if survivor.SpouseUIN != 0 {
		t.Fatalf("deleted spouse left survivor projection %d", survivor.SpouseUIN)
	}
}

func TestKinLifecycleProjectsAuthoritativeMembership(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index, uin := range []uint32{1_000_001, 1_000_002, 1_000_003} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		profile.Nickname = []string{"糖一", "糖二", "糖三"}[index]
		if _, err := store.LoadOrCreate(ctx, uin, profile); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateKin(ctx, 1_000_001, "无凭证家族", "", 0, 1); !errors.Is(err, ErrKinCreationItemMissing) {
		t.Fatalf("create kin without qualification = %v", err)
	}
	owner, err := store.Load(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	owner.Inventory = append(owner.Inventory, game.NewPermanentItemInfo(KinSuperAllianceBookItemID, 1))
	if err := store.Save(ctx, 1_000_001, owner); err != nil {
		t.Fatal(err)
	}
	kin, err := store.CreateKin(ctx, 1_000_001, "糖家族", "我爱我家", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if kin.Declaration != "我爱我家" || kin.FlagID != defaultKinFlagID() ||
		KinMemberCapacity(kin.Status) != KinDefaultMemberCapacity || kin.Title != DefaultKinAuthorityTitle() {
		t.Fatalf("created kin base = %+v", kin)
	}
	owner, err = store.Load(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if owner.KinIndex != kin.Index || owner.KinName != "糖家族" {
		t.Fatalf("owner kin projection = %+v", owner)
	}
	if err := store.ApplyToKin(ctx, 1_000_002, kin.Index); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptKinApplication(ctx, 1_000_001, 1_000_002, kin.Index); err != nil {
		t.Fatal(err)
	}
	member, err := store.Load(ctx, 1_000_002)
	if err != nil {
		t.Fatal(err)
	}
	if member.KinIndex != kin.Index || member.KinName != "糖家族" {
		t.Fatalf("member kin projection = %+v", member)
	}
	for _, authority := range []uint32{KinAuthorityGuardian, KinAuthorityHallMaster, KinAuthorityElder} {
		if err := store.SetKinMemberAuthority(ctx, 1_000_001, 1_000_002, kin.Index, authority); err != nil {
			t.Fatalf("promote member to %d: %v", authority, err)
		}
	}
	if err := store.SetKinMemberAuthority(ctx, 1_000_001, 1_000_001, kin.Index, KinAuthorityElder); !errors.Is(err, ErrKinPermissionDenied) {
		t.Fatalf("change immutable owner authority = %v", err)
	}
	if _, err := store.SetKinTitle(ctx, 1_000_002, kin.Index, "掌门\x07长老\x07堂主\x07护法\x07弟子\x07"); !errors.Is(err, ErrKinPermissionDenied) {
		t.Fatalf("elder changed owner-only position names = %v", err)
	}
	if _, err := store.SetKinTitle(ctx, 1_000_001, kin.Index, "掌门\x07元老\x07堂主\x07护法\x07弟子\x07"); err != nil {
		t.Fatalf("owner changes position names: %v", err)
	}
	if _, err := store.SetKinNotification(ctx, 1_000_002, kin.Index, "今晚八点集合"); !errors.Is(err, ErrKinPermissionDenied) {
		t.Fatalf("elder changed owner-only notification = %v", err)
	}
	if _, err := store.SetKinNotification(ctx, 1_000_001, kin.Index, "今晚八点集合"); err != nil {
		t.Fatalf("owner changes notification: %v", err)
	}
	if _, err := store.SetKinDeclaration(ctx, 1_000_001, kin.Index, "欢迎加入糖家族"); err != nil {
		t.Fatal(err)
	}
	updatedKin, err := store.LoadKin(ctx, kin.Index)
	if err != nil {
		t.Fatal(err)
	}
	positionNames, titleErr := decodeKinAuthorityTitles(updatedKin.Title)
	if updatedKin.Declaration != "欢迎加入糖家族" || updatedKin.Notification != "今晚八点集合" ||
		titleErr != nil || !reflect.DeepEqual(positionNames, []string{"掌门", "元老", "堂主", "护法", "弟子"}) {
		t.Fatalf("persisted kin texts = declaration %q, notification %q", updatedKin.Declaration, updatedKin.Notification)
	}
	if err := store.ApplyToKin(ctx, 1_000_003, kin.Index); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptKinApplication(ctx, 1_000_002, 1_000_003, kin.Index); err != nil {
		t.Fatal(err)
	}
	members, err := store.ListKinMembers(ctx, kin.Index)
	if err != nil || len(members) != 3 {
		t.Fatalf("members = %+v, err=%v", members, err)
	}
	if err := store.KickKinMember(ctx, 1_000_002, 1_000_003, kin.Index); !errors.Is(err, ErrKinPermissionDenied) {
		t.Fatalf("elder kicked member despite owner-only native command = %v", err)
	}
	if err := store.KickKinMember(ctx, 1_000_001, 1_000_003, kin.Index); err != nil {
		t.Fatalf("owner kicks member: %v", err)
	}
	left, err := store.Load(ctx, 1_000_003)
	if err != nil {
		t.Fatal(err)
	}
	if left.KinIndex != 0 || left.KinName != "" {
		t.Fatalf("left player still has kin projection: %+v", left)
	}
	if err := store.DismissKin(ctx, 1_000_001, kin.Index); err != nil {
		t.Fatal(err)
	}
	for _, uin := range []uint32{1_000_001, 1_000_002} {
		profile, err := store.Load(ctx, uin)
		if err != nil {
			t.Fatal(err)
		}
		if profile.KinIndex != 0 || profile.KinName != "" {
			t.Fatalf("dismissed member %d still has kin projection: %+v", uin, profile)
		}
	}
}

func TestKinAuthorityTitleStoresLogicalEditorNames(t *testing.T) {
	want := "族长\x07长老\x07堂主\x07护法\x07弟子\x07"
	if got := DefaultKinAuthorityTitle(); got != want {
		t.Fatalf("default logical authority title = %q, want %q", got, want)
	}
	legacy := "5 12 5 族长 10 5 长老 8 5 堂主 6 5 护法 4 5 弟子 "
	got, err := normalizeKinAuthorityTitle(legacy)
	if err != nil || got != want {
		t.Fatalf("normalize schema-18 authority title = %q, %v", got, err)
	}
	schema20 := "5 12 4 族长10 4 长老8 4 堂主6 4 护法4 4 弟子"
	got, err = normalizeKinAuthorityTitle(schema20)
	if err != nil || got != want {
		t.Fatalf("normalize schema-20 authority title = %q, %v", got, err)
	}
	positions, err := decodeKinAuthorityTitles(got)
	if err != nil || !reflect.DeepEqual(positions, defaultKinAuthorityTitles) {
		t.Fatalf("decode logical authority title = %v, %v", positions, err)
	}
}

func TestPlayerStoreMigratesSchema20ASCIIKinAuthorityTitles(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "players.sqlite")
	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = append(profile.Inventory, game.NewPermanentItemInfo(KinSuperAllianceBookItemID, 1))
	if err := store.Save(ctx, uin, profile); err != nil {
		store.Close()
		t.Fatal(err)
	}
	kin, err := store.CreateKin(ctx, uin, "二十版家族", "", 0x10, 1)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	const schema20Title = "5 12 4 族长10 4 长老8 4 堂主6 4 护法4 4 弟子"
	if _, err := store.db.ExecContext(ctx, `UPDATE kin_families SET title = ? WHERE kin_index = ?`, schema20Title, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE schema_version SET version = 20`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migrated, err := store.LoadKin(ctx, kin.Index)
	if err != nil || migrated.Title != DefaultKinAuthorityTitle() {
		t.Fatalf("schema-20 title migration = %q, %v", migrated.Title, err)
	}
	var version int
	if err := store.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version); err != nil || version != currentPlayerStoreSchemaVersion {
		t.Fatalf("migrated schema version = %d, %v", version, err)
	}
}

func TestDeleteKinOwnerDismissesFamilyTransactionally(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index, uin := range []uint32{1_000_001, 1_000_002} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		profile.Nickname = []string{"族长", "成员"}[index]
		if _, err := store.LoadOrCreate(ctx, uin, profile); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := store.Load(ctx, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	owner.Inventory = append(owner.Inventory, game.NewPermanentItemInfo(KinSuperAllianceBookItemID, 1))
	if err := store.Save(ctx, 1_000_001, owner); err != nil {
		t.Fatal(err)
	}
	kin, err := store.CreateKin(ctx, 1_000_001, "待解散家族", "", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyToKin(ctx, 1_000_002, kin.Index); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptKinApplication(ctx, 1_000_001, 1_000_002, kin.Index); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Delete(ctx, 1_000_001)
	if err != nil || !deleted {
		t.Fatalf("Delete(owner) = %v, %v", deleted, err)
	}
	if _, err := store.LoadKin(ctx, kin.Index); !errors.Is(err, ErrKinNotFound) {
		t.Fatalf("LoadKin after owner deletion = %v", err)
	}
	member, err := store.Load(ctx, 1_000_002)
	if err != nil {
		t.Fatal(err)
	}
	if member.KinIndex != 0 || member.KinName != "" || member.KinFlagID != (game.KinFlagID{}) {
		t.Fatalf("member projection survived owner deletion: %+v", member)
	}
}

func TestKinCreationRequiresFullAllianceBookQuantity(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = append(profile.Inventory, game.NewPermanentItemInfo(KinAllianceBookItemID, KinAllianceBookRequired-1))
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateKin(ctx, uin, "数量不足", "", 0, 1); !errors.Is(err, ErrKinCreationItemMissing) {
		t.Fatalf("CreateKin with 99 alliance books = %v", err)
	}
	profile, err = store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	for index := range profile.Inventory {
		if profile.Inventory[index].ItemID == KinAllianceBookItemID {
			profile.Inventory[index].NumOfItem = KinAllianceBookRequired
		}
	}
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateKin(ctx, uin, "数量足够", "", 0, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseInventoryItemDeductsAndGrantsAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.Money = 2500
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	purchased, err := store.PurchaseInventoryItem(ctx, uin, game.NewPermanentItemInfo(2067, 1), 1000, false)
	if err != nil {
		t.Fatal(err)
	}
	if purchased.GameInfo.Money != 1500 || len(purchased.Inventory) != 1 || purchased.Inventory[0].ItemID != 2067 || purchased.Inventory[0].NumOfItem != 1 {
		t.Fatalf("purchased profile = %+v", purchased)
	}
	if _, err := store.PurchaseInventoryItem(ctx, uin, game.NewPermanentItemInfo(2067, 1), 1000, false); !errors.Is(err, ErrInventoryItemOwned) {
		t.Fatalf("duplicate cosmetic error = %v", err)
	}
	reloaded, err := store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GameInfo.Money != 1500 {
		t.Fatalf("money changed after rejected duplicate = %d", reloaded.GameInfo.Money)
	}
	if _, err := store.PurchaseInventoryItem(ctx, uin, game.NewPermanentItemInfo(20043, 2), 1000, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PurchaseInventoryItem(ctx, uin, game.NewPermanentItemInfo(20043, 2), 1000, true); !errors.Is(err, ErrInsufficientGameMoney) {
		t.Fatalf("insufficient-money error = %v", err)
	}
	reloaded, err = store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	item, found, err := store.InventoryItem(ctx, uin, 20043)
	if err != nil || !found || item.NumOfItem != 2 || reloaded.GameInfo.Money != 500 {
		t.Fatalf("atomic rejection profile/item = money:%d item:%+v found:%v err:%v", reloaded.GameInfo.Money, item, found, err)
	}
}

func TestDeletePetClearsEquippedPetReferenceAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	pet, err := store.GrantPet(ctx, uin, 25_001, "普通的酷比")
	if err != nil {
		t.Fatal(err)
	}
	profile, err = store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	profile.GameInfo.PetID = pet.PetID
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	removed, err := store.DeletePet(ctx, uin, pet.PetID)
	if err != nil || !removed {
		t.Fatalf("DeletePet = %t, %v", removed, err)
	}
	profile, err = store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	if profile.GameInfo.PetID != 0 {
		t.Fatalf("equipped pet reference = %d, want 0", profile.GameInfo.PetID)
	}
	pets, err := store.ListPets(ctx, uin)
	if err != nil || len(pets) != 0 {
		t.Fatalf("pets after removal = %+v, %v", pets, err)
	}
}

func TestPetCardAndSkillBookTransactions(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		game.NewPermanentItemInfo(28001, 2),
		game.NewPermanentItemInfo(27001, 1),
		game.NewPermanentItemInfo(27011, 1),
	}
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}

	pet, remaining, err := store.AdoptPetFromInventory(ctx, uin, 28001, 25001, "普通的酷比")
	if err != nil || remaining != 1 || pet.PetTypeID != 25001 {
		t.Fatalf("adopted pet=%+v remaining=%d err=%v", pet, remaining, err)
	}
	if _, _, err = store.AdoptPetFromInventory(ctx, uin, 28001, 25001, "普通的酷比"); !errors.Is(err, ErrPetAlreadyOwned) {
		t.Fatalf("duplicate adoption error = %v", err)
	}
	card, found, err := store.InventoryItem(ctx, uin, 28001)
	if err != nil || !found || card.NumOfItem != 1 {
		t.Fatalf("rejected adoption consumed card: item=%+v found=%t err=%v", card, found, err)
	}

	pet, remaining, err = store.LearnPetSkillFromInventory(ctx, uin, pet.PetID, 27001, 51, 1)
	if err != nil || remaining != 0 || len(pet.Skills) != 1 || pet.Skills[0] != 51 {
		t.Fatalf("learned skill pet=%+v remaining=%d err=%v", pet, remaining, err)
	}
	if _, _, err = store.LearnPetSkillFromInventory(ctx, uin, pet.PetID, 27011, 81, 1); !errors.Is(err, ErrPetSkillLevelOccupied) {
		t.Fatalf("same-level skill error = %v", err)
	}
	book, found, err := store.InventoryItem(ctx, uin, 27011)
	if err != nil || !found || book.NumOfItem != 1 {
		t.Fatalf("rejected skill consumed book: item=%+v found=%t err=%v", book, found, err)
	}

	pet, err = store.SetPetActive(ctx, uin, pet.PetID, true)
	if err != nil || pet.PetState != game.PetStateActive {
		t.Fatalf("activate pet=%+v err=%v", pet, err)
	}
	profile, err = store.Load(ctx, uin)
	if err != nil || profile.GameInfo.PetID != pet.PetID {
		t.Fatalf("active profile pet=%d err=%v", profile.GameInfo.PetID, err)
	}
	firstPetID := pet.PetID
	secondPet, err := store.GrantPet(ctx, uin, 25002, "富有的酷比")
	if err != nil {
		t.Fatal(err)
	}
	secondPet, err = store.SetPetActive(ctx, uin, secondPet.PetID, true)
	if err != nil || secondPet.PetState != game.PetStateActive {
		t.Fatalf("activate second pet=%+v err=%v", secondPet, err)
	}
	pets, err := store.ListPets(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	activeCount := 0
	for _, storedPet := range pets {
		if storedPet.PetState == game.PetStateActive {
			activeCount++
			if storedPet.PetID != secondPet.PetID {
				t.Fatalf("active pet ID=%d, want %d", storedPet.PetID, secondPet.PetID)
			}
		}
		if storedPet.PetID == firstPetID && storedPet.PetState != game.PetStateInactive {
			t.Fatalf("previous pet remains active: %+v", storedPet)
		}
	}
	if activeCount != 1 {
		t.Fatalf("active pet count=%d, want 1", activeCount)
	}
	if _, err := store.db.Exec(`UPDATE player_pets SET state = ? WHERE uin = ? AND pet_id = ?`, game.PetStateActive, uin, firstPetID); err == nil {
		t.Fatal("database allowed two active pets for one account")
	}
	profile, err = store.Load(ctx, uin)
	if err != nil || profile.GameInfo.PetID != secondPet.PetID {
		t.Fatalf("second active profile pet=%d, want %d, err=%v", profile.GameInfo.PetID, secondPet.PetID, err)
	}
	pet, err = store.RenamePet(ctx, uin, pet.PetID, "糖宠")
	if err != nil || pet.PetName != "糖宠" {
		t.Fatalf("renamed pet=%+v err=%v", pet, err)
	}
}

func TestPetSlotExpansionControlsAccountCapacity(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	if err := store.Save(ctx, uin, game.DefaultPlayerProfile()); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < game.PlayerPetsBaseCapacity; index++ {
		if _, err := store.GrantPet(ctx, uin, uint32(25_100+index), "base"); err != nil {
			t.Fatalf("grant base pet %d: %v", index, err)
		}
	}
	if _, err := store.GrantPet(ctx, uin, 25_200, "overflow"); err == nil {
		t.Fatal("base account accepted a sixth pet")
	}
	if err := store.SetInventoryItem(ctx, uin, game.NewPermanentItemInfo(uint16(itemeffect.PetSlotExpansion10ItemID), 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPet(ctx, uin, 25_200, "expanded"); err != nil {
		t.Fatalf("10-slot expansion did not raise capacity: %v", err)
	}
	if err := store.SetInventoryItem(ctx, uin, game.NewPermanentItemInfo(uint16(itemeffect.PetSlotExpansion20ItemID), 1)); err != nil {
		t.Fatal(err)
	}
	var lastPet game.PetInfo
	for index := 0; index < 10; index++ {
		lastPet, err = store.GrantPet(ctx, uin, uint32(25_201+index), "extended")
		if err != nil {
			t.Fatalf("grant extended pet %d: %v", index, err)
		}
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := playerPetCapacityTx(ctx, tx, uin)
	_ = tx.Rollback()
	if err != nil || capacity != game.PlayerPetsCapacity {
		t.Fatalf("20-slot expansion capacity = %d, %v", capacity, err)
	}
	if err := store.SetInventoryItem(ctx, uin, game.ItemInfo{ItemID: uint16(itemeffect.PetSlotExpansion20ItemID)}); !errors.Is(err, ErrPetCapacityReduction) {
		t.Fatalf("removing 20-slot expansion with 16 pets = %v", err)
	}
	if _, found, err := store.InventoryItem(ctx, uin, uint16(itemeffect.PetSlotExpansion20ItemID)); err != nil || !found {
		t.Fatalf("rejected 20-slot removal persisted: found=%v, err=%v", found, err)
	}
	if removed, err := store.DeletePet(ctx, uin, lastPet.PetID); err != nil || !removed {
		t.Fatalf("delete sixteenth pet = %v, %v", removed, err)
	}
	if err := store.SetInventoryItem(ctx, uin, game.ItemInfo{ItemID: uint16(itemeffect.PetSlotExpansion20ItemID)}); err != nil {
		t.Fatalf("remove 20-slot expansion at 15 pets: %v", err)
	}
	if err := store.SetInventoryItem(ctx, uin, game.ItemInfo{ItemID: uint16(itemeffect.PetSlotExpansion10ItemID)}); !errors.Is(err, ErrPetCapacityReduction) {
		t.Fatalf("removing 10-slot expansion with 15 pets = %v", err)
	}
	profile, err := store.Load(ctx, uin)
	if err != nil {
		t.Fatal(err)
	}
	profile.Inventory = nil
	if err := store.Save(ctx, uin, profile); !errors.Is(err, ErrPetCapacityReduction) {
		t.Fatalf("full profile save bypassed pet capacity reduction: %v", err)
	}
	if _, found, err := store.InventoryItem(ctx, uin, uint16(itemeffect.PetSlotExpansion10ItemID)); err != nil || !found {
		t.Fatalf("rejected 10-slot removal persisted: found=%v, err=%v", found, err)
	}
}

func TestFeedPetAppliesOriginalAxesAndLevelsAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(26002, 2)}
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	pet, err := store.GrantPet(ctx, uin, 25001, "普通的酷比")
	if err != nil {
		t.Fatal(err)
	}
	thresholds := []uint32{0, 20, 50}
	pet, remaining, err := store.FeedPetFromInventory(ctx, uin, pet.PetID, 26002,
		petcatalog.FoodEffect{Experience: 20, Loyalty: 400}, thresholds)
	if err != nil || remaining != 1 || pet.PetExperience != 20 || pet.PetLevel != 2 || pet.PetLoyalty != game.PetMaxLoyalty {
		t.Fatalf("fed pet=%+v remaining=%d err=%v", pet, remaining, err)
	}
	pet, remaining, err = store.FeedPetFromInventory(ctx, uin, pet.PetID, 26002,
		petcatalog.FoodEffect{Experience: 40, Loyalty: 400}, thresholds)
	if err != nil || remaining != 0 || pet.PetExperience != 50 || pet.PetLevel != 3 {
		t.Fatalf("capped pet=%+v remaining=%d err=%v", pet, remaining, err)
	}
	if _, _, err = store.FeedPetFromInventory(ctx, uin, pet.PetID, 26002,
		petcatalog.FoodEffect{Experience: 20}, thresholds); !errors.Is(err, ErrInventoryItemMissing) {
		t.Fatalf("exhausted food error = %v", err)
	}
	pets, err := store.ListPets(ctx, uin)
	if err != nil || len(pets) != 1 || pets[0].PetExperience != 50 {
		t.Fatalf("failed feed changed pet: %+v err=%v", pets, err)
	}
}

func TestPlayerStoreTransfersOwnedItemBetweenRoleLoadouts(t *testing.T) {
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seedProfile := game.DefaultPlayerProfile()
	seedProfile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(22, 1)}
	if _, err := store.LoadOrCreate(context.Background(), 1_000_001, seedProfile); err != nil {
		t.Fatal(err)
	}
	seed := equipment.Seed{
		Key: "cosmetics-v1", UIN: 1_000_001,
		Inventory: []equipment.SeedItem{{ItemID: 22, Quantity: 1}},
		Assignments: []equipment.Assignment{
			{RoleID: 1, Slot: equipment.SlotHeadFront, ItemID: 22},
		},
	}
	applied, err := store.ApplyEquipmentSeed(context.Background(), seed)
	if err != nil || !applied {
		t.Fatalf("ApplyEquipmentSeed = %v, %v", applied, err)
	}
	if applied, err = store.ApplyEquipmentSeed(context.Background(), seed); err != nil || applied {
		t.Fatalf("second ApplyEquipmentSeed = %v, %v", applied, err)
	}
	assignments, err := store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 || assignments[0].RoleID != 1 || assignments[0].ItemID != 22 {
		t.Fatalf("assignments = %+v", assignments)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), 1_000_001, []equipment.Change{{
		RoleID: 7, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true,
	}}); err != nil {
		t.Fatal(err)
	}
	assignments, err = store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 1 || assignments[0].RoleID != 7 {
		t.Fatalf("assignments after transfer to role 7 = %+v, %v", assignments, err)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), 1_000_001, []equipment.Change{{
		RoleID: 7, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: false,
	}}); err != nil {
		t.Fatal(err)
	}
	assignments, err = store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("assignments after unequip = %+v, %v", assignments, err)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), 1_000_001, []equipment.Change{{
		RoleID: 1, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInventoryItem(context.Background(), 1_000_001, game.ItemInfo{ItemID: 22}); err != nil {
		t.Fatal(err)
	}
	assignments, err = store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("removing owned cosmetic left role loadouts = %+v, %v", assignments, err)
	}
}

func TestPlayerStoreMigratesSharedLoadoutToLastEquippedRole(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	path := filepath.Join(t.TempDir(), "players.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(game.DefaultPlayerProfile())
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
		`INSERT INTO schema_version(version) VALUES(6)`,
		`CREATE TABLE local_players (uin INTEGER PRIMARY KEY, profile_json TEXT NOT NULL, created_utc TEXT NOT NULL, updated_utc TEXT NOT NULL)`,
		`CREATE TABLE player_loadouts (uin INTEGER NOT NULL, role_id INTEGER NOT NULL, slot TEXT NOT NULL, item_id INTEGER NOT NULL, PRIMARY KEY (uin, role_id, slot))`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO local_players VALUES(1000001, ?, 'before', 'before')`, string(profileJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO player_loadouts VALUES
		(1000001, 7, 'namecard_bound', 12503),
		(1000001, 9, 'namecard_bound', 12503)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assignments, err := store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 1 || assignments[0].RoleID != 9 || assignments[0].ItemID != 12503 {
		t.Fatalf("migrated assignments = %+v, %v", assignments, err)
	}
	if _, err := store.db.Exec(`INSERT INTO player_loadouts VALUES(1000001, 7, 'namecard_bound', 12503)`); err == nil {
		t.Fatal("schema accepted a second role for the same inventory item")
	}
}

func TestPlayerStoreMigrationRemovesPersistedSelectedRole(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	path := filepath.Join(t.TempDir(), "players.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.RoleID = 23
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
		`INSERT INTO schema_version(version) VALUES(8)`,
		`CREATE TABLE local_players (uin INTEGER PRIMARY KEY, profile_json TEXT NOT NULL, created_utc TEXT NOT NULL, updated_utc TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO local_players VALUES(1000001, ?, 'before', 'before')`, string(profileJSON)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var encoded string
	if err := store.db.QueryRow(`SELECT profile_json FROM local_players WHERE uin = 1000001`).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, `"role_id"`) {
		t.Fatalf("persisted selected role survived migration: %s", encoded)
	}
	loaded, err := store.Load(context.Background(), 1_000_001)
	if err != nil || loaded.GameInfo.RoleID != 0 {
		t.Fatalf("loaded selected role = %d, %v", loaded.GameInfo.RoleID, err)
	}
}

func TestPlayerStoreMigratesLegacyJSONInventoryToTypedTable(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	path := filepath.Join(t.TempDir(), "players.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
		`INSERT INTO schema_version(version) VALUES(1)`,
		`CREATE TABLE local_players (uin INTEGER PRIMARY KEY, profile_json TEXT NOT NULL, created_utc TEXT NOT NULL, updated_utc TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	legacy := game.DefaultPlayerProfile()
	legacy.Inventory = []game.ItemInfo{{ItemID: game.SinglePlayerAdventureCardItemID, NumOfItem: 7}}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_players(uin, profile_json, created_utc, updated_utc) VALUES(1000001, ?, 'before', 'before')`, string(encoded)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile, err := store.Load(context.Background(), 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Inventory) != 1 || profile.Inventory[0] != game.NewPermanentItemInfo(game.SinglePlayerAdventureCardItemID, 7) {
		t.Fatalf("migrated inventory = %+v", profile.Inventory)
	}
	var version int
	if err := store.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != currentPlayerStoreSchemaVersion {
		t.Fatalf("schema version = %d, err=%v", version, err)
	}
}

func TestPlayerStoreMigratesReversedDefaultKinFlag(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "players.sqlite")
	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	profile := game.DefaultPlayerProfile()
	profile.Inventory = append(profile.Inventory, game.NewPermanentItemInfo(KinSuperAllianceBookItemID, 1))
	if err := store.Save(ctx, 1_000_001, profile); err != nil {
		store.Close()
		t.Fatal(err)
	}
	kin, err := store.CreateKin(ctx, 1_000_001, "迁移家族", "", 0, 1)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE kin_families SET flag_id = X'0000000100000000' WHERE kin_index = ?`, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE schema_version SET version = 15`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migrated, err := store.LoadKin(ctx, kin.Index)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.FlagID != defaultKinFlagID() || migrated.FlagID.Index() != 0 || migrated.FlagID.FlagID() != defaultKinBadgeID {
		t.Fatalf("migrated KINFLAGID = % X (index=%d flag=%d)", migrated.FlagID, migrated.FlagID.Index(), migrated.FlagID.FlagID())
	}
}

func TestPlayerStoreMigratesNativeKinAuthorityAndCapacityContract(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "players.sqlite")
	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for index, uin := range []uint32{1_000_001, 1_000_002} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		if index == 0 {
			profile.Inventory = append(profile.Inventory, game.NewPermanentItemInfo(KinSuperAllianceBookItemID, 1))
		}
		if err := store.Save(ctx, uin, profile); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	kin, err := store.CreateKin(ctx, 1_000_001, "旧职位家族", "", 0x10, 1)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.ApplyToKin(ctx, 1_000_002, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.AcceptKinApplication(ctx, 1_000_001, 1_000_002, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE kin_families SET status = 16, title = '' WHERE kin_index = ?`, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE kin_members SET grade = 1, authority_id = CASE WHEN uin = 1000001 THEN 2 ELSE 0 END WHERE kin_index = ?`, kin.Index); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE schema_version SET version = 16`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migrated, err := store.LoadKin(ctx, kin.Index)
	if err != nil {
		t.Fatal(err)
	}
	if KinMemberCapacity(migrated.Status) != KinDefaultMemberCapacity || migrated.Title != DefaultKinAuthorityTitle() {
		t.Fatalf("migrated kin base = %+v", migrated)
	}
	members, err := store.ListKinMembers(ctx, kin.Index)
	if err != nil || len(members) != 2 {
		t.Fatalf("migrated members = %+v, err=%v", members, err)
	}
	if members[0].UIN != 1_000_001 || members[0].AuthorityID != KinAuthorityOwner || members[0].Grade != KinAuthorityOwner ||
		members[1].UIN != 1_000_002 || members[1].AuthorityID != KinAuthorityMember || members[1].Grade != KinAuthorityMember {
		t.Fatalf("migrated member grades = %+v", members)
	}
	if err := store.DismissKin(ctx, 1_000_001, kin.Index); err != nil {
		t.Fatalf("dismiss migrated kin: %v", err)
	}
}

func TestPlayerStoreMigratesLegacyPermanentPeriodForShopQuantity(t *testing.T) {
	t.Skip("historical migration removed; the player database is already on the final schema")
	path := filepath.Join(t.TempDir(), "players.sqlite")
	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	profile := game.DefaultPlayerProfile()
	if err := store.Save(context.Background(), 1_000_001, profile); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO player_inventory(
		uin, item_id, quantity, item_status, item_role_id, item_effect, item_color, buy_time, available_period
	) VALUES(1000001, 20043, 500, 0, 0, 0, 0, 1234, 2147483647)`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE schema_version SET version = 9`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item, found, err := store.InventoryItem(context.Background(), 1_000_001, 20043)
	if err != nil || !found {
		t.Fatalf("migrated item found=%t err=%v", found, err)
	}
	if item.NumOfItem != 500 || item.BuyTime != 0 || item.AvailPeriod != game.LocalPermanentAvailablePeriod {
		t.Fatalf("migrated permanent item = %+v", item)
	}
}

func TestPlayerStoreGMAccountAndInventoryOperations(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, uin := range []uint32{1_000_002, 1_000_001} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(uin - 1_000_000)
		if err := store.Save(ctx, uin, profile); err != nil {
			t.Fatal(err)
		}
	}
	uins, err := store.ListUINs(ctx)
	if err != nil || len(uins) != 2 || uins[0] != 1_000_001 || uins[1] != 1_000_002 {
		t.Fatalf("ListUINs = %v, %v", uins, err)
	}
	item := game.NewPermanentItemInfo(20043, 500)
	if err := store.SetInventoryItem(ctx, 1_000_001, item); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.InventoryItem(ctx, 1_000_001, 20043)
	if err != nil || !found || got.NumOfItem != 500 {
		t.Fatalf("InventoryItem = %+v/%v, %v", got, found, err)
	}
	if err := store.SetInventoryItem(ctx, 1_000_001, game.ItemInfo{ItemID: 20043}); err != nil {
		t.Fatal(err)
	}
	if _, found, err = store.InventoryItem(ctx, 1_000_001, 20043); err != nil || found {
		t.Fatalf("removed InventoryItem found=%v, err=%v", found, err)
	}
	deleted, err := store.Delete(ctx, 1_000_002)
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	if deleted, err = store.Delete(ctx, 1_000_002); err != nil || deleted {
		t.Fatalf("second Delete = %v, %v", deleted, err)
	}
}

func TestReconcileLearnedRecipesIndexesStatusFourBooks(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const (
		uin       = uint32(1_000_001)
		bookID    = uint16(24_050)
		productID = uint16(20_050)
	)
	profile := game.DefaultPlayerProfile()
	book := game.NewPermanentItemInfo(bookID, 1)
	book.ItemStatus = game.ItemStatusLearnedRecipe
	profile.Inventory = []game.ItemInfo{book}
	if err := store.Save(ctx, uin, profile); err != nil {
		t.Fatal(err)
	}
	catalog := &craftcatalog.CombineCatalog{
		Version: 110,
		Recipes: []craftcatalog.CombineRecipe{{
			Category: "工具", Name: "医疗包", BookItemID: bookID, ProductItemID: productID,
			Materials: []craftcatalog.CombineMaterial{{ItemID: 30_001, Name: "材料", Quantity: 1}},
		}},
	}
	if err := store.ReconcileLearnedRecipes(ctx, uin, catalog); err != nil {
		t.Fatal(err)
	}
	var indexedBook uint16
	if err := store.db.QueryRowContext(ctx,
		`SELECT book_item_id FROM player_recipes WHERE uin = ? AND product_item_id = ?`, uin, productID,
	).Scan(&indexedBook); err != nil {
		t.Fatal(err)
	}
	if indexedBook != bookID {
		t.Fatalf("indexed book = %d, want %d", indexedBook, bookID)
	}
	book.ItemStatus = 0
	if err := store.SetInventoryItem(ctx, uin, book); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileLearnedRecipes(ctx, uin, catalog); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM player_recipes WHERE uin = ? AND product_item_id = ?`, uin, productID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale learned recipe count = %d", count)
	}
}
