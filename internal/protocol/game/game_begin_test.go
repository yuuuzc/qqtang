package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func testGameBeginData() GameBeginData {
	return GameBeginData{
		GameID: 1, MapID: 1, SpawnSeed: 1, ItemSeed: 1, ArbitratorPlayerID: 1,
		Players:    []PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1}},
		ContinueID: NoRemoteContinueFileID, GameTimeMS: DefaultAdventureGameTimeMS,
	}
}

func TestGameBeginDataControlledItemArraysRoundTrip(t *testing.T) {
	data := testGameBeginData()
	data.Players[0].NewItems = []GameItemType{{ItemID: 20100, Quantity: 1}}
	data.Items = []GameItemType{{ItemID: 20044, Quantity: 2}, {ItemID: 20045, Quantity: 1}}
	data.NewItems = []GameItemType{{ItemID: 20020, Quantity: 3}}
	payload, err := data.MarshalLengthPrefixedNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseLengthPrefixedGameBeginDataNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Players[0].NewItems) != 1 || decoded.Players[0].NewItems[0] != data.Players[0].NewItems[0] {
		t.Fatalf("player new items = %+v", decoded.Players[0].NewItems)
	}
	if len(decoded.Items) != 2 || decoded.Items[0] != data.Items[0] || decoded.Items[1] != data.Items[1] {
		t.Fatalf("ordinary item types = %+v", decoded.Items)
	}
	if len(decoded.NewItems) != 1 || decoded.NewItems[0] != data.NewItems[0] {
		t.Fatalf("new item types = %+v", decoded.NewItems)
	}
	roundTrip, err := decoded.MarshalLengthPrefixedNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, payload) {
		t.Fatalf("controlled item arrays round trip mismatch\n got: %x\nwant: %x", roundTrip, payload)
	}
	if _, err := ParseLengthPrefixedGameBeginDataNetwork(payload[:len(payload)-1]); err == nil {
		t.Fatal("expected truncated GAME_BEGIN_DATA to fail")
	}
}

func TestGameBeginDataRejectsInvalidItemPools(t *testing.T) {
	data := testGameBeginData()
	data.Items = []GameItemType{{ItemID: 20044, Quantity: GameBeginMaxExpandedItems + 1}}
	if _, err := data.MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted an ordinary item pool larger than the client's 64-item expansion")
	}
	data.Items = []GameItemType{{ItemID: 20044, Quantity: 0}}
	if _, err := data.MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted zero GAME_ITEM_TYPE quantity")
	}
	data.Items = make([]GameItemType, GameBeginMaxItemTypes+1)
	for index := range data.Items {
		data.Items[index] = GameItemType{ItemID: uint32(20000 + index), Quantity: 1}
	}
	if _, err := data.MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted too many GAME_BEGIN_DATA item types")
	}
}

func TestGameBeginDataRejectsMoreThanEightPlayers(t *testing.T) {
	data := testGameBeginData()
	data.Players = make([]PlayerGameInfo, GameBeginMaxPlayerCount+1)
	for index := range data.Players {
		data.Players[index] = PlayerGameInfo{PlayerID: uint16(index + 1), RoleID: 1, TeamID: 1}
	}
	if _, err := data.MarshalNetworkBinary(); err == nil {
		t.Fatal("accepted more players than the native GAME_BEGIN_DATA array")
	}
}

func TestBuildLocalAdventureGameBegin(t *testing.T) {
	requestPlaintext, err := hex.DecodeString(capturedStartGamePlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, requestPlaintext)
	data := testGameBeginData()
	packet, err := BuildLocalAdventureGameBeginWithReader(request, data, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command != GameBeginNotifyCommand {
		t.Fatalf("command = 0x%04X, want 0x%04X", decoded.Command, GameBeginNotifyCommand)
	}
	if got, want := binary.BigEndian.Uint32(packet[6:10]), binary.BigEndian.Uint32(request[6:10]); got != want {
		t.Fatalf("game-begin transport sequence = 0x%08X, want preserved 0x%08X", got, want)
	}
	if got, want := binary.BigEndian.Uint16(decoded.Plaintext[6:8]), binary.BigEndian.Uint16(requestPlaintext[6:8]); got != want {
		t.Fatalf("game-begin inner sequence = 0x%04X, want preserved 0x%04X", got, want)
	}
	wantPayload, err := data.MarshalLengthPrefixedNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Plaintext[localInnerHeaderSize:]; !bytes.Equal(got, wantPayload) {
		t.Fatalf("payload = %x, want %x", got, wantPayload)
	}
}

func TestAdventureGameBeginCarriesExtendedPointsAndSelectedMap(t *testing.T) {
	var mapHash [GameBeginMapHashSize]byte
	copy(mapHash[:], "0123456789ABCDEF0123456789ABCDEF")
	data, err := NewAdventureGameBeginData(GameBeginOptions{
		GameID: 9, MapID: 1612, SpawnSeed: 11, ItemSeed: 12, ArbitratorPlayerID: 1, ContinueID: 3,
		MapHash: mapHash, GameTimeMS: DefaultAdventureGameTimeMS,
		Players: []PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1, ExtPoint: 5_200_000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := data.MarshalLengthPrefixedNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseLengthPrefixedGameBeginDataNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.MapID != 1612 || decoded.ContinueID != 3 || decoded.Players[0].ExtPoint != 5_200_000 {
		t.Fatalf("decoded adventure game = %+v", decoded)
	}
}

func TestAdventureGameBeginRejectsArbitratorOutsidePlayers(t *testing.T) {
	_, err := NewAdventureGameBeginData(GameBeginOptions{
		GameID: 9, MapID: 1612, SpawnSeed: 11, ItemSeed: 12,
		ArbitratorPlayerID: 7,
		GameTimeMS:         DefaultAdventureGameTimeMS,
		Players:            []PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1}},
	})
	if err == nil {
		t.Fatal("accepted arbitrator that is absent from GAME_BEGIN players")
	}
}
