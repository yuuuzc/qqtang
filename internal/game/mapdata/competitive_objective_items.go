package mapdata

import "qqtang/internal/clientdata/sceneelement"

// CompetitiveObjectiveSceneItems returns the rule-owned scene objects that
// make a native battlefield playable. They are deliberately separate from a
// map's ordinary WallItemRules: item fields suppress only action-slot wall
// pickups, and cannot suppress the gems or sculpture fragments that define
// rules 5 and 6 themselves.
//
// Treasure has one shared map pool. The native target is 22 points per member
// of a team, with scene values 1/2/3. The 8:4:2 distribution below is the
// minimal original-style mix whose value is exactly 22 for each team member.
// teamMemberCount is the largest active team so a free/unbalanced room never
// starts with less than the largest native target.
//
// Sculpture's installed resource set has one ordinary fragment (0xA1) and
// three drawable special fragments (0xA2..0xA4). Client.exe also has a factory
// branch for 0xA5/material 16, but this client build has no Item16_stand.img;
// advertising it creates a collidable, pickable object with no world sprite.
// The recovered original rule uses six ordinary fragments and chooses one
// special appearance per round. itemSeed makes that choice deterministic for
// every client without inventing coordinates.
//
// Tank's repair, armour and shell objects are wall rewards too. Keeping them
// in this rule-owned group merges them with the recovered ordinary pool in the
// same GAME_BEGIN.NewItems list; a second coordinate-bearing 0x0FAE path would
// create two independent scene histories.
func (entry CompetitiveMap) CompetitiveObjectiveSceneItems(teamMemberCount int, itemSeed uint32) []CompetitiveWallItem {
	if teamMemberCount < 1 {
		return nil
	}
	switch entry.Rule {
	case CompetitiveRuleTreasure:
		return []CompetitiveWallItem{
			{SceneID: uint32(sceneelement.TreasureGemOne), Quantity: int16(8 * teamMemberCount)},
			{SceneID: uint32(sceneelement.TreasureGemTwo), Quantity: int16(4 * teamMemberCount)},
			{SceneID: uint32(sceneelement.TreasureGemThree), Quantity: int16(2 * teamMemberCount)},
		}
	case CompetitiveRuleSculpture:
		special := [...]sceneelement.ID{
			sceneelement.SculptureFragment13,
			sceneelement.SculptureFragment14,
			sceneelement.SculptureFragment15,
		}[itemSeed%3]
		return []CompetitiveWallItem{
			{SceneID: uint32(sceneelement.SculptureFragment12), Quantity: 6},
			{SceneID: uint32(special), Quantity: 1},
		}
	case CompetitiveRuleTank:
		return []CompetitiveWallItem{
			{SceneID: uint32(sceneelement.TankRepair), Quantity: int16(teamMemberCount)},
			{SceneID: uint32(sceneelement.TankArmor), Quantity: int16(teamMemberCount)},
			{SceneID: uint32(sceneelement.TankShell), Quantity: int16(teamMemberCount)},
		}
	default:
		return nil
	}
}
