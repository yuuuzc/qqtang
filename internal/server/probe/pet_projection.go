package probe

import "qqtang/internal/protocol/game"

// projectPetForClient joins immutable, species-level innate talents to the
// learned skill bytes stored for this owned pet. Innate talents come from the
// original pet-card description/PetCfg mapping and deliberately never cross
// the persistence boundary.
func (server *Server) projectPetForClient(pet game.PetInfo) game.PetInfo {
	innate := server.petInnateSkillsByType[pet.PetTypeID]
	projected := make([]byte, 0, len(innate)+len(pet.Skills))
	seen := make(map[byte]struct{}, len(innate)+len(pet.Skills))
	for _, skillID := range innate {
		if skillID == 0 || skillID > 48 {
			continue
		}
		if _, duplicate := seen[skillID]; duplicate {
			continue
		}
		seen[skillID] = struct{}{}
		projected = append(projected, skillID)
	}
	for _, skillID := range pet.Skills {
		// 1..48 are immutable innate talents. Ignore legacy persisted copies so
		// that projection remains idempotent; learned skill-book IDs start at 51.
		if skillID <= 48 {
			continue
		}
		if _, duplicate := seen[skillID]; duplicate {
			continue
		}
		seen[skillID] = struct{}{}
		projected = append(projected, skillID)
	}
	pet.Skills = projected
	return pet
}

func (server *Server) projectPetsForClient(pets []game.PetInfo) []game.PetInfo {
	projected := make([]game.PetInfo, len(pets))
	for index, pet := range pets {
		projected[index] = server.projectPetForClient(pet)
	}
	return projected
}
