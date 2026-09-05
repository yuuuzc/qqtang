package game

import "testing"

func TestShippedProgressionBoundaries(t *testing.T) {
	if len(CompetitiveRanks()) != 180 || len(AdventureRanks()) != 31 {
		t.Fatalf("rank counts = %d/%d", len(CompetitiveRanks()), len(AdventureRanks()))
	}
	rank179, err := CompetitiveRankByDegree(179)
	if err != nil {
		t.Fatal(err)
	}
	if rank179.Title != "???" || rank179.SubLevel != 5 || rank179.MinPoints != 738_920_000 || rank179.MaxPoints != 887_624_999 {
		t.Fatalf("competitive rank 179 = %+v", rank179)
	}
	if rank := CompetitiveRankForPoints(1_621_150_000); rank.Degree != 180 || rank.MinPoints != 887_625_000 || rank.MaxPoints != MaxPlayerExperience {
		t.Fatalf("competitive point projection = %+v", rank)
	}
	if rank := AdventureRankForPoints(428_326_799); rank.Level != 30 || rank.MinPoints != MaxAdventureExperience {
		t.Fatalf("adventure point projection = %+v", rank)
	}
}

func TestApplyCompetitiveExperienceDerivesLevelAndCaps(t *testing.T) {
	rank179, err := CompetitiveRankByDegree(179)
	if err != nil {
		t.Fatal(err)
	}
	info, change := ApplyCompetitiveExperience(GameInfo{
		Point: rank179.MaxPoints, Degree: 1,
	}, 1)
	if info.Point != rank179.MaxPoints+1 || info.Degree != 180 {
		t.Fatalf("competitive result = %+v", info)
	}
	if !change.LeveledUp || change.PreviousLevel != 179 || change.CurrentLevel != 180 || change.Capped {
		t.Fatalf("competitive level change = %+v", change)
	}

	info, change = ApplyCompetitiveExperience(info, MaxPlayerExperience)
	if info.Point != MaxPlayerExperience || info.Degree != MaxPlayerLevel || !change.Capped {
		t.Fatalf("competitive capped result/change = %+v / %+v", info, change)
	}
}

func TestApplyAdventureExperienceDerivesLevelAndCaps(t *testing.T) {
	level29 := AdventureRanks()[29]
	info, change := ApplyAdventureExperience(GameInfo{ExtPoint: level29.MaxPoints}, 1)
	if info.ExtPoint != MaxAdventureExperience || !change.LeveledUp || change.PreviousLevel != 29 || change.CurrentLevel != 30 {
		t.Fatalf("adventure level change = %+v / %+v", info, change)
	}

	info, change = ApplyAdventureExperience(info, 100)
	if info.ExtPoint != MaxAdventureExperience || change.AppliedPoints != 0 || !change.Capped {
		t.Fatalf("adventure capped result/change = %+v / %+v", info, change)
	}
}
