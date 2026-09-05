package petcatalog

import "testing"

func TestParsePetDefinitions(t *testing.T) {
	contents := `[PetExperience]
ExpTable=0,180,580

#测试宠物
[Pet25001]
Level1=100
Level4=1
Level7=2
`
	definitions, experience, err := parse(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Name != "测试宠物" || definitions[0].PetTypeID != 25001 {
		t.Fatalf("definitions = %+v", definitions)
	}
	if len(experience) != 3 || experience[2] != 580 {
		t.Fatalf("experience = %v", experience)
	}
}

func TestParseSkillsUsesSparseClientIDsAndLevels(t *testing.T) {
	contents := `[SkillName]
skill1=糖币之光10%
skill51=糖币加加1级
skill54=糖币加加2级
skill81=勇气加加1级
[SkillDescription]
skill51=宠物一级技能
`
	skills, err := parseSkills(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 4 || skills[1].SkillID != 51 || skills[1].Level != 1 || !skills[1].Learned || skills[2].Level != 2 {
		t.Fatalf("skills = %+v", skills)
	}
	if level, ok := LearnedSkillLevel(81); !ok || level != 1 {
		t.Fatalf("LearnedSkillLevel(81) = %d, %t", level, ok)
	}
}

func TestParseFoodEffectRejectsUnknownOriginalNumbers(t *testing.T) {
	effect, complete := parseFoodEffect("增加宠物体力400点，增加宠物成长值20点。")
	if !complete || effect.Loyalty != 400 || effect.Experience != 20 {
		t.Fatalf("effect = %+v, complete=%t", effect, complete)
	}
	if _, complete = parseFoodEffect("增加宠物体力。"); complete {
		t.Fatal("food without an original numeric value was accepted")
	}
}

func TestDeriveInnateSkillsUsesPetDescriptionAndCapsAtThree(t *testing.T) {
	skills := []SkillDefinition{
		{SkillID: 1, Name: "糖币之光10%"},
		{SkillID: 4, Name: "声望之光5%"},
		{SkillID: 5, Name: "材料闪耀5%"},
		{SkillID: 47, Name: "亲密闪耀5%"},
		{SkillID: 51, Name: "糖币加加1级", Learned: true},
	}
	got, err := deriveInnateSkills("含着奶嘴。具有天赋：糖币之光10%，声望之光5%，亲密闪耀5%。", skills)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 4 || got[2] != 47 {
		t.Fatalf("innate skills = %v, want [1 4 47]", got)
	}
	if got, err = deriveInnateSkills("没什么天赋。", skills); err != nil || len(got) != 0 {
		t.Fatalf("no-talent pet = %v, %v", got, err)
	}
	if _, err = deriveInnateSkills("具有天赋：糖币加加1级。", skills); err == nil {
		t.Fatal("learned skill-book skill was accepted as an innate talent")
	}
}
