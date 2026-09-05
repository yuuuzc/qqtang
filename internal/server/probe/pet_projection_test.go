package probe

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestPetInnateSkillsAreProjectedWithoutChangingPersistedSkills(t *testing.T) {
	server := &Server{petInnateSkillsByType: map[uint32][]byte{25096: {4, 3, 2}}}
	stored := game.PetInfo{PetID: 7, PetTypeID: 25096, Skills: []byte{51}}
	projected := server.projectPetForClient(stored)
	if len(projected.Skills) != 4 || projected.Skills[0] != 4 || projected.Skills[1] != 3 || projected.Skills[2] != 2 || projected.Skills[3] != 51 {
		t.Fatalf("projected skills = %v, want [4 3 2 51]", projected.Skills)
	}
	if len(stored.Skills) != 1 || stored.Skills[0] != 51 {
		t.Fatalf("stored skills mutated = %v", stored.Skills)
	}
	// A legacy persisted innate byte is ignored rather than duplicated.
	stored.Skills = []byte{4, 51}
	projected = server.projectPetForClient(stored)
	if len(projected.Skills) != 4 {
		t.Fatalf("idempotent projected skills = %v", projected.Skills)
	}
}
