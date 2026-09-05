package petcatalog

const preservedPetVideoURL = "https://www.bilibili.com/video/BV1Zy4y1u7ZT/"

// hiddenModelFamilies preserves late official model families whose original
// PetTypeIDs are unavailable. LocalPetTypeID is a reserved, explicitly local
// extension identity installed into PetCfg.ini; it is never presented as an
// original Tencent ID or confused with a pet-card item ID.
func hiddenModelFamilies() []ModelFamily {
	return []ModelFamily{
		modelFamily(25901, "毛毛兽", 101, 102),
		modelFamily(25902, "树苗", 103, 104),
		modelFamily(25903, "帝黑星", 105, 106),
		modelFamily(25904, "哆唻咪海豚", 107, 108),
		modelFamily(25905, "兔子酷比", 109, 110),
		modelFamily(25906, "九尾狐", 111, 112),
		modelFamily(25907, "糖果国王", 113, 114),
		modelFamily(25908, "宝石小鬼", 115, 116),
		modelFamily(25909, "圣诞老人", 117, 118),
		modelFamily(25910, "胖小丁", 119, 120),
		modelFamily(25911, "龙宝宝", 121, 122),
		modelFamily(25912, "独角兽2", 123, 124),
		modelFamily(25913, "孙小圣", 125, 0),
	}
}

func modelFamily(localPetTypeID uint32, name string, juvenile, adult uint32) ModelFamily {
	return ModelFamily{
		LocalPetTypeID: localPetTypeID, Name: name, JuvenileResourceID: juvenile, AdultResourceID: adult,
		Source: SourceCommunityIndex, EvidenceURL: preservedPetVideoURL,
		Assignable: false,
		Reason:     "尚未把保留的本地 PetTypeID 扩展安装到客户端 PetCfg.ini",
	}
}
