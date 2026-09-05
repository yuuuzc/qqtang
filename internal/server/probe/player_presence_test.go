package probe

import (
	"testing"

	"qqtang/internal/server/persistence"
)

func TestProtocolKinMembersProjectsLiveStatusIndependentlyOfAuthority(t *testing.T) {
	members := []persistence.KinMember{
		{UIN: 1_000_001, Nickname: "one", Status: 0, AuthorityID: persistence.KinAuthorityOwner},
		{UIN: 1_000_002, Nickname: "two", Status: 3, AuthorityID: persistence.KinAuthorityMember},
	}
	projected := protocolKinMembers(members, map[uint32]struct{}{1_000_001: {}})
	if projected[0].Status&1 == 0 {
		t.Fatalf("online owner status = %d, want bit 0 set", projected[0].Status)
	}
	if projected[1].Status&1 != 0 {
		t.Fatalf("offline member status = %d, want bit 0 clear", projected[1].Status)
	}
	if got := projected[0].Status >> 24 & 0x0f; got != persistence.KinAuthorityOwner {
		t.Fatalf("owner authority nibble = %d, want %d", got, persistence.KinAuthorityOwner)
	}
	if got := projected[1].Status >> 24 & 0x0f; got != persistence.KinAuthorityMember {
		t.Fatalf("member authority nibble = %d, want %d", got, persistence.KinAuthorityMember)
	}
	const unrelatedStatusBits = uint32(0xf0fffffe)
	if projected[1].Status&unrelatedStatusBits != members[1].Status&unrelatedStatusBits {
		t.Fatalf("unrelated status bits changed from %08x to %08x", members[1].Status, projected[1].Status)
	}
	if projected[0].Grade != persistence.KinAuthorityOwner || projected[1].Grade != persistence.KinAuthorityMember {
		t.Fatalf("wire authority grades = %d/%d", projected[0].Grade, projected[1].Grade)
	}
}
