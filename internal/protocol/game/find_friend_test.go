package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedFindFriendPlaintext = "00670000000000150004ffffffff000f424109fe27f0000f4242"

func TestDecodeCapturedFindFriendRequest(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedFindFriendPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeLocalFindFriendRequest(makeLocalPacketForTest(t, plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.Time != 0x09FE27F0 || request.FriendUIN != 1_000_002 {
		t.Fatalf("find-friend request = %+v", request)
	}
}

func TestBuildFindFriendResponseWithVariableInventory(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedFindFriendPlaintext)
	response := FindFriendResponse{
		FriendUIN: 1_000_002, PlayerName: "糖二", PlayerID: 2, Gender: 0,
		ServerID: 1, DialogID: 1, SectionID: 1, Status: FindFriendStatusOnline,
		GameInfo: GameInfo{Degree: 179, RoleID: 2, Point: 1_621_150_000},
		Items:    []ItemInfo{NewPermanentItemInfo(99, 500)}, Honor: 7,
	}
	packet, err := BuildLocalFindFriendResponseWithReader(
		makeLocalPacketForTest(t, plaintext), response, bytes.NewReader(make([]byte, 64)),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qqtea.Decrypt(packet[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	body := decoded[localInnerHeaderSize:]
	if binary.BigEndian.Uint16(body[0:2]) != 0 || binary.BigEndian.Uint32(body[2:6]) != 1_000_002 {
		t.Fatalf("response prefix = %x", body[:6])
	}
	if body[6] != 0xCC || body[7] != 0xC7 || binary.BigEndian.Uint16(body[26:28]) != 2 {
		t.Fatalf("GBK name/player ID fields = %x", body[6:28])
	}
	if got := binary.BigEndian.Uint16(body[112:114]); got != 1 {
		t.Fatalf("item count = %d, want 1", got)
	}
	if got, want := len(body), 114+ItemInfoBinarySize+4+2+KinFlagIDSize+4+1; got != want {
		t.Fatalf("response body length = %d, want %d", got, want)
	}
}
