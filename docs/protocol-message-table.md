# QQTMsgData 完整消息字段表

由 `cmd/qqt-msgtable` 从 `runtime/client-patched/QQTMsgData.bin` 确定性提取。二进制大小 `407316` 字节，SHA256 `20E22F42183F9BD2304A7B24E384FE7AA058F476B0A631B3E10A75414AD441BC`，共 `438` 条定义。JSON 保存全部消息头和字段原始 DWORD；CSV 每行一个字段，适合筛选。

| Index | Name | Category | Schema | Encoded/Max | Fields | Field layout (`name@offset:width[capacity]`) |
|---:|---|---:|---:|---:|---:|---|
| 0 | `REQUEST_SPARK` | `0x00001100` | `0x00000BEA` | 623/209 | 4 | Uin@0:4, TargetUin@4:4, SparkWordLength@8:1, SparkWord@9:1[200]{count@8} |
| 1 | `RESPONSE_SPARK` | `0x00001100` | `0x00000BEB` | 624/211 | 5 | ResultID@0:2, Uin@2:4, TargetUin@6:4, ResultWordLength@10:1, ResultWord@11:1[200]{count@10} |
| 2 | `NOTIFY_SPARK` | `0x00001100` | `0x00000BEC` | 625/229 | 5 | Uin@0:4, TargetUin@4:4, SparkerNickname@8:1[20], SparkWordLength@28:1, SparkWord@29:1[200]{count@28} |
| 3 | `REQUEST_ANSWER_SPARK` | `0x00001140` | `0x00000BED` | 626/10 | 3 | TargetUin@0:4, Uin@4:4, ResultID@8:2 |
| 4 | `RESPONSE_ANSWER_SPARK` | `0x00001100` | `0x00000BEE` | 627/211 | 5 | ResultID@0:2, Uin@2:4, TargetUin@6:4, ResultWordLength@10:1, ResultWord@11:1[200]{count@10} |
| 5 | `REQUEST_MARRIAGE_INFO` | `0x00001140` | `0x00000BEF` | 628/8 | 2 | Uin@0:4, TargetUin@4:4 |
| 6 | `RESPONSE_MARRIAGE_INFO` | `0x00001100` | `0x00000BF0` | 629/270 | 12 | ResultID@0:2, TargetUin@2:4, MarriageUin@6:4, MarriageLoyalty@10:4, MarriageLevel@14:2, LevelValue@16:4, MarriageAge@20:4, LoveWordLength@24:2, LoveWord@26:1[200]{count@24}, Nickname@226:1[20], SpouseNickname@246:1[20], RingID@266:4 |
| 7 | `REQUEST_MODIFY_LOVEWORD` | `0x00001100` | `0x00000BF1` | 630/210 | 4 | Uin@0:4, SpouseUin@4:4, LoveWordLength@8:2, LoveWord@10:1[200]{count@8} |
| 8 | `RESPONSE_MODIFY_LOVEWORD` | `0x00001140` | `0x00000BF2` | 631/10 | 3 | Uin@0:4, TargetUin@4:4, ResultID@8:2 |
| 9 | `REQUEST_DIVORCE` | `0x00001140` | `0x00000BF3` | 632/8 | 2 | Uin@0:4, TargetUin@4:4 |
| 10 | `RESPONSE_DIVORCE` | `0x00001140` | `0x00000BF4` | 633/10 | 3 | Uin@0:4, TargetUin@4:4, ResultID@8:2 |
| 11 | `REQUEST_START_WEDDING` | `0x00001140` | `0x00000BF5` | 634/10 | 3 | Uin@0:4, TargetUin@4:4, WeddingModeID@8:2 |
| 12 | `RESPONSE_START_WEDDING` | `0x00001100` | `0x00000BF6` | 635/253 | 8 | Uin@0:4, TargetUin@4:4, NickName@8:1[20], TargetNickName@28:1[20], ResultID@48:2, ResultWordLength@50:1, ResultWord@51:1[200]{count@50}, WeddingModeID@251:2 |
| 13 | `REQUEST_CHANGE_WEDMODE` | `0x00001140` | `0x00000BFA` | 639/6 | 2 | Uin@0:4, WeddingModeID@4:2 |
| 14 | `RESPONSE_CHANGE_WEDMODE` | `0x00001100` | `0x00000BFB` | 640/207 | 4 | Uin@0:4, ResultID@4:2, ResultWordLength@6:1, ResultWord@7:1[200]{count@6} |
| 15 | `NOTIFY_CHANGE_WEDMODE` | `0x00001140` | `0x00000BFC` | 641/6 | 2 | Uin@0:4, WeddingModeID@4:2 |
| 16 | `NOTIFY_WEDDING_CONFIRM` | `0x00001100` | `0x00000BF7` | 636/249 | 6 | Uin@0:4, TargetUin@4:4, NickName@8:1[20], TargetNickName@28:1[20], NotifyWordLength@48:1, NotifyWord@49:1[200]{count@48} |
| 17 | `REQUEST_ANSWER_WEDDING` | `0x00001140` | `0x00000BF8` | 637/10 | 3 | Uin@0:4, TargetUin@4:4, ResultID@8:2 |
| 18 | `RESPONSE_ANSWER_WEDDING` | `0x00001100` | `0x00000BF9` | 638/251 | 7 | Uin@0:4, TargetUin@4:4, NickName@8:1[20], TargetNickName@28:1[20], ResultID@48:2, ResultWordLength@50:1, ResultWord@51:1[200]{count@50} |
| 19 | `PATTERN_POINT` | `0x00001140` | `0xFFFFFFFF` | 4294967295/11 | 4 | GameMode@0:1, PatternPoint@1:4, PatternLevel@5:2, LevelValue@7:4 |
| 20 | `PATTERN_POINTS` | `0x00001100` | `0xFFFFFFFF` | 4294967295/166 | 2 | PatternNum@0:1, PatternPoints@1:11[15]{count@0} |
| 21 | `KinFlagId` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 2 | Index@0:4, FlagID@4:4 |
| 22 | `KinBase` | `0x00001100` | `0xFFFFFFFF` | 4294967295/758 | 26 | KinIndex@0:4, Uin@4:4, Time@8:4, DismissTime@12:4, Status@16:4, Grade@20:4, KinFlagID@24:8, MemberNum@32:2, ContentLen@34:2, TitleLen@36:2, Name@38:1[17], Content@55:1[257]{count@34}, Title@312:1[200]{count@36}, KinSection@512:4, BaseUpdate@516:4, ListUpdate@520:4, NotificationLen@524:2, Notification@526:1[200]{count@524}, Honor@726:4, ActivePoint@730:4, LastHonor@734:4, LastActivePoint@738:4, LastHonorOrder@742:4, LastActivePointOrder@746:4, BeforeHonorOrder@750:4, BeforeActivePointOrder@754:4 |
| 23 | `KinMemInfoOld` | `0x00001100` | `0xFFFFFFFF` | 4294967295/50 | 9 | Uin@0:4, NickNameLen@4:2, NickName@6:1[20]{count@4}, Time@26:4, Status@30:4, StatusTime@34:4, Grade@38:4, OnlineTime@42:4, LastLoginTime@46:4 |
| 24 | `KinFlag` | `0x00001100` | `0xFFFFFFFF` | 4294967295/12 | 2 | KinIndex@0:4, KinFlagID@4:8 |
| 25 | `KIN_MEM_ATTACH_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 2 | Honor@0:4, ActivePoint@4:4 |
| 26 | `PLAYER_INFO_IN_ROOM_ATTACH` | `0x00001140` | `0xFFFFFFFF` | 4294967295/12 | 3 | Honor@0:4, Attach1@4:4, Attach2@8:4 |
| 27 | `PLAYER_INFO_ATTACH` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 2 | Honor@0:4, Attach1@4:4 |
| 28 | `REQUEST_CREATE_KIN` | `0x00001100` | `0x00001771` | 542/294 | 8 | Uin@0:4, Time@4:4, Status@8:4, Reserve@12:2, ContentLen@14:2, Name@16:1[17], Content@33:1[257]{count@14}, KinSection@290:4 |
| 29 | `RESPONSE_CREATE_KIN` | `0x00001100` | `0x00001772` | 543/214 | 5 | Uin@0:4, KinIndex@4:4, ErrorNo@8:4, ReasonLen@12:2, ReasonStr@14:1[200]{count@12} |
| 30 | `REQUEST_FETCH_KIN_BASE` | `0x00001140` | `0x00001773` | 544/16 | 4 | Uin@0:4, Time@4:4, KinIndex@8:4, IsMember@12:4 |
| 31 | `RESPONSET_FETCH_KIN_BASE` | `0x00001140` | `0x00001774` | 545/762 | 2 | Uin@0:4, kinBase@4:758 |
| 32 | `REQUEST_FETCH_KIN_MEMLIST` | `0x00001140` | `0x00001775` | 546/16 | 4 | Uin@0:4, Time@4:4, KinIndex@8:4, ListUpdate@12:4 |
| 33 | `RESPONSE_FETCH_KIN_MEMLIST_OLD` | `0x00001100` | `0x00001776` | 547/11612 | 6 | Uin@0:4, ListUpdate@4:4, Result@8:2, MemListNum@10:2, MemList@12:50[200]{count@10}, MemAttachList@10012:8[200]{count@10} |
| 34 | `REQUEST_UPDATE_KIN_TITLE` | `0x00001100` | `0x00001777` | 548/214 | 5 | Uin@0:4, Time@4:4, KinIndex@8:4, TitleLen@12:2, Title@14:1[200]{count@12} |
| 35 | `RESPONSE_UPDATE_KIN_TITLE` | `0x00001100` | `0x00001778` | 549/208 | 4 | Uin@0:4, Result@4:2, TitleLen@6:2, Title@8:1[200]{count@6} |
| 36 | `REQUEST_UPDATE_KIN_FLAG` | `0x00001100` | `0x00001779` | 550/2066 | 6 | Uin@0:4, Time@4:4, KinIndex@8:4, FlagID@12:4, FlagSize@16:2, FlagPic@18:1[2048]{count@16} |
| 37 | `RESPONSE_UPDATE_KIN_FLAG` | `0x00001140` | `0x0000177A` | 551/6 | 2 | Uin@0:4, Result@4:2 |
| 38 | `REQUEST_OPR_KIN` | `0x00001140` | `0x0000177B` | 552/24 | 6 | Uin@0:4, Time@4:4, KinIndex@8:4, dstUin@12:4, Para@16:4, Reserve@20:4 |
| 39 | `RESPONSE_OPR_KIN` | `0x00001100` | `0x0000177C` | 553/538 | 8 | Uin@0:4, Time@4:4, KinIndex@8:4, dstUin@12:4, Para@16:4, Reserve@20:4, ReasonDesLen@24:2, ReasonDesStr@26:1[512]{count@24} |
| 40 | `NOTIFY_OPR_KIN` | `0x00001140` | `0x0000177D` | 554/782 | 7 | Uin@0:4, Time@4:4, KinIndex@8:4, dstUin@12:4, Para@16:4, Reserve@20:4, kinBase@24:758 |
| 41 | `NOTIFY_UPDATE_KIN_FLAG` | `0x00001100` | `0x0000177E` | 555/1206 | 3 | Uin@0:4, KinFlagNum@4:2, kinFlag@6:12[100]{count@4} |
| 42 | `KinOrderInfo` | `0x00001100` | `0xFFFFFFFF` | 4294967295/30 | 6 | KinIndex@0:4, NameLength@4:1, Name@5:1[17]{count@4}, Value@22:4, Order@26:2, BeforeOrder@28:2 |
| 43 | `TopKinInfo` | `0x00001140` | `0xFFFFFFFF` | 4294967295/1268 | 5 | OrderTime@0:4, KinOrderInfo@4:30[40], GroupNum@1204:2, GroupMemberNum@1206:2, SameGroupKinInfo@1208:30[2] |
| 44 | `PET_BASE_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/58 | 10 | PetId@0:4, PetTypeId@4:4, PetExperience@8:4, PetLoyalty@12:4, PetLevel@16:2, PetMood@18:2, PetState@20:2, PetName@22:1[12], SkillCount@34:4, Skills@38:1[20]{count@34} |
| 45 | `REQUEST_HANDLE_PET` | `0x00001140` | `0x00000BCD` | 373/36 | 5 | Uin@0:4, Time@4:4, EventID@8:4, PetID@12:4, ParaBuf@16:1[20] |
| 46 | `RESPONSE_HANDLE_PET` | `0x00001100` | `0x00000BCE` | 374/266 | 5 | ResultID@0:2, EventID@2:4, ReasonLen@6:2, Reason@8:1[200]{count@6}, PetInfo@208:58 |
| 47 | `REQUEST_PLAYER_PETS_INFO` | `0x00001140` | `0x00000BCF` | 375/8 | 2 | Uin@0:4, Time@4:4 |
| 48 | `RESPONSE_PLAYER_PETS_INFO` | `0x00001100` | `0x00000BD0` | 376/1452 | 4 | Count@0:1, PetsInfo@1:58[5]{count@0}, ExCount@291:1, ExPetsInfo@292:58[20]{count@291} |
| 49 | `SALE_ITEM` | `0x00001140` | `0xFFFFFFFF` | 4294967295/20 | 5 | SaleIndex@0:4, SrcItemID@4:4, SrcItemNum@8:4, DstItemID@12:4, DstItemNum@16:4 |
| 50 | `REQUEST_ITEM_BYUIN` | `0x00001140` | `0x00000BD1` | 377/16 | 4 | Uin@0:4, Time@4:4, DstUin@8:4, SaleType@12:4 |
| 51 | `RESPONSE_ITEM_BYUIN` | `0x00001100` | `0x00000BD2` | 378/1218 | 8 | Uin@0:4, ResultID@4:2, DstUin@6:4, SaleType@10:4, TotalCount@14:2, ItemCount@16:2, Item@18:20[50]{count@16}, ItemPeriod@1018:4[50]{count@16} |
| 52 | `REQUEST_ITEM_BYID` | `0x00001140` | `0x00000BD3` | 379/20 | 6 | Uin@0:4, Time@4:4, SaleType@8:4, ItemID@12:4, FromIndex@16:2, ToIndex@18:2 |
| 53 | `RESPONSE_ITEM_BYID` | `0x00001100` | `0x00000BD4` | 380/622 | 9 | Uin@0:4, Result@4:2, SaleType@6:4, ItemID@10:4, FromIndex@14:2, ToIndex@16:2, TotalCount@18:2, ItemCount@20:2, Item@22:20[30]{count@20} |
| 54 | `REQUEST_PET_INFO_ONSALE` | `0x00001140` | `0x00000BD5` | 381/12 | 3 | Uin@0:4, Time@4:4, SaleIndex@8:4 |
| 55 | `RESPONSE_PET_INFO_ONSALE` | `0x00001100` | `0x00000BD6` | 382/269 | 6 | Uin@0:4, Result@4:2, SaleIndex@6:4, petinfo@10:58, ReasonLen@68:1, Reason@69:1[200]{count@68} |
| 56 | `SERVER_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/12 | 4 | ServerID@0:4, ServerIP@4:4, ServerPort@8:2, ServerUdpPort@10:2 |
| 57 | `SECTION_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/44 | 9 | SectionNameLen@0:1, SectionName@1:1[23]{count@0}, SectionID@24:2, ServerID@26:4, MaxNumOfPlayer@30:2, CurrentNumOfPlayer@32:2, LowPoint@34:4, HighPoint@38:4, LocationID@42:2 |
| 58 | `CHANNEL_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/17626 | 4 | ChannelName@0:1[20], ChannelID@20:4, NumOfSection@24:2, Sections@26:44[400]{count@24} |
| 59 | `CHANNEL_SECTION_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/17603 | 3 | ChannelID@0:2, SectionCount@2:1, Sections@3:44[400]{count@2} |
| 60 | `ITEM_INFO` | `0x00001540` | `0xFFFFFFFF` | 4294967295/18 | 8 | ItemID@0:2, NumOfItem@2:4, ItemStatus@6:1, ItemRoleID@7:1, ItemEffect@8:1, ItemColor@9:1, BuyTime@10:4, AvailPeriod@14:4 |
| 61 | `GAME_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/47 | 13 | WinNum@0:4, LossNum@4:4, EqualNum@8:4, OrgID@12:4, Point@16:4, Money@20:4, Degree@24:2, RoleID@26:1, PetID@27:4, ExtWinNum@31:4, ExtLossNum@35:4, ExtEqualNum@39:4, ExtPoint@43:4 |
| 62 | `ROOM_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/32 | 8 | NameLen@0:1, RoomName@1:1[20]{count@0}, RoomID@21:2, RoomFlag@23:1, MapID@24:2, NumOfPlayer@26:1, GameType@27:1, ContinueID@28:4 |
| 63 | `PLAYER_INFO_OLD` | `0x00001100` | `0xFFFFFFFF` | 4294967295/231 | 13 | PlayerUin@0:4, PlayerNickname@4:1[20], PlayerID@24:2, Gender@26:1, IconID@27:1, Identity@28:4, GameInfo@32:47, ExtItemNum@79:1, ExtItemID@80:4[30]{count@79}, KinIndex@200:4, KinNameLen@204:2, KinName@206:1[17]{count@204}, KinFlagID@223:8 |
| 64 | `PLAYER_INFO_IN_ROOM_OLD` | `0x00001500` | `0x0000042A` | 319/9172 | 16 | Uin@0:4, QQNickname@4:1[20], PlayerID@24:2, TermAndSeat@26:1, Status@27:1, Gender@28:1, IconID@29:1, Identity@30:4, GameInfo@34:47, ItemCount@81:2, Items@83:18[500]{count@81}, KinIndex@9083:4, KinNameLen@9087:2, KinName@9089:1[17]{count@9087}, KinFlagID@9106:8, pet@9114:58 |
| 65 | `REQUEST_SERVEROPR_KIN` | `0x00001140` | `0x0000177F` | 556/782 | 7 | Uin@0:4, Time@4:4, KinIndex@8:4, DstUin@12:4, Para@16:4, Reserve@20:4, KinBase@24:758 |
| 66 | `RESPONSE_SERVEROPR_KIN` | `0x00001100` | `0x00001780` | 557/1296 | 9 | Uin@0:4, Time@4:4, KinIndex@8:4, DstUin@12:4, Para@16:4, Reserve@20:4, KinBase@24:758, ReasonDesLen@782:2, ReasonDesStr@784:1[512]{count@782} |
| 67 | `NOTIFY_SERVERNOTIFYOPR_KIN` | `0x00001140` | `0x00001781` | 558/782 | 7 | Uin@0:4, Time@4:4, KinIndex@8:4, DstUin@12:4, Para@16:4, Reserve@20:4, KinBase@24:758 |
| 68 | `REQUEST_SEND_KINMSG` | `0x00001100` | `0x00001782` | 559/528 | 5 | Uin@0:4, Time@4:4, KinIndex@8:4, KinMsgLen@12:4, KinMsg@16:1[512]{count@12} |
| 69 | `NOTIFY_SEND_KINMSG` | `0x00001100` | `0x00001783` | 560/524 | 4 | Uin@0:4, KinIndex@4:4, KinMsgLen@8:4, KinMsg@12:1[512]{count@8} |
| 70 | `REQUEST_KIN_KICKOUT` | `0x00001140` | `0x00001784` | 561/16 | 4 | Uin@0:4, Time@4:4, DstUin@8:4, KinIndex@12:4 |
| 71 | `RESPONSE_KIN_KICKOUT` | `0x00001100` | `0x00001785` | 562/528 | 6 | Result@0:2, Uin@2:4, DstUin@6:4, KinIndex@10:4, ReasonDesLen@14:2, ReasonDesStr@16:1[512]{count@14} |
| 72 | `REQUEST_KIN_EXIT` | `0x00001140` | `0x00001786` | 563/12 | 3 | Uin@0:4, Time@4:4, KinIndex@8:4 |
| 73 | `RESPONSE_KIN_EXIT` | `0x00001100` | `0x00001787` | 564/524 | 5 | Result@0:2, Uin@2:4, KinIndex@6:4, ReasonDesLen@10:2, ReasonDesStr@12:1[512]{count@10} |
| 74 | `REQUEST_DISMISS_KIN` | `0x00001140` | `0x00001788` | 565/16 | 4 | Uin@0:4, Time@4:4, KinIndex@8:4, KinOperationID@12:4 |
| 75 | `RESPONSE_DISMISS_KIN` | `0x00001100` | `0x00001789` | 566/528 | 6 | Result@0:2, Uin@2:4, KinIndex@6:4, KinOperationID@10:4, ReasonDesLen@14:2, ReasonDesStr@16:1[512]{count@14} |
| 76 | `NOTIFY_KIN_EVENT` | `0x00001100` | `0x0000178A` | 567/262 | 10 | EventType@0:4, KinIndex@4:4, Uin@8:4, NickNameLen@12:2, NickName@14:1[20]{count@12}, AttachUin@34:4, AttachNickNameLen@38:2, AttachNickName@40:1[20]{count@38}, KinEventDescriptionLen@60:2, KinEventDescription@62:1[200]{count@60} |
| 77 | `REQUEST_KINMEMBERAUTHORITY` | `0x00001140` | `0x0000178B` | 568/20 | 5 | KinIndex@0:4, Uin@4:4, Time@8:4, DstUin@12:4, AuthorityID@16:4 |
| 78 | `RESPONSE_KINMEMBERAUTHORITY` | `0x00001100` | `0x0000178C` | 569/532 | 7 | ResultID@0:2, KinIndex@2:4, Uin@6:4, DstUin@10:4, AuthorityID@14:4, ReasonDesLen@18:2, ReasonDesStr@20:1[512]{count@18} |
| 79 | `REQUEST_SETKINAUTHORITY` | `0x00001100` | `0x0000178D` | 570/214 | 5 | KinIndex@0:4, Uin@4:4, Time@8:4, TitleLen@12:2, Title@14:1[200]{count@12} |
| 80 | `RESPONSE_SETKINAUTHORITY` | `0x00001100` | `0x0000178E` | 571/722 | 6 | ResultID@0:2, KinIndex@2:4, TitleLen@6:2, Title@8:1[200]{count@6}, ReasonDesLen@208:2, ReasonDesStr@210:1[512]{count@208} |
| 81 | `REQUEST_SETKINBEDGE` | `0x00001140` | `0x0000178F` | 572/20 | 5 | KinIndex@0:4, Uin@4:4, Time@8:4, KinBedgeID@12:4, KinDefineBedgeID@16:4 |
| 82 | `RESPONSE_SETKINBEDGE` | `0x00001140` | `0x00001790` | 573/18 | 5 | ResultID@0:2, KinIndex@2:4, Uin@6:4, KinBedgeID@10:4, KinDefineBedgeID@14:4 |
| 83 | `CFGFILE_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/32016 | 7 | FileID@0:4, Reserve@4:1, ResultID@5:1, Version@6:2, FileLen@8:4, FileZipLen@12:4, FileBuffer@16:1[32000]{count@12} |
| 84 | `NOTIFY_CFG_FILE` | `0x00001100` | `0x00001792` | 574/96052 | 2 | FileNum@0:4, CfgfileInfos@4:32016[3]{count@0} |
| 85 | `REQUEST_SETKINDECLARE` | `0x00001100` | `0x00001793` | 575/271 | 5 | Uin@0:4, Time@4:4, KinIndex@8:4, ContentLen@12:2, Content@14:1[257]{count@12} |
| 86 | `RESPONSE_SETKINDECLARE` | `0x00001100` | `0x00001794` | 576/783 | 7 | ResultID@0:2, Uin@2:4, KinIndex@6:4, ContentLen@10:2, Content@12:1[257]{count@10}, ReasonDesLen@269:2, ReasonDesStr@271:1[512]{count@269} |
| 87 | `REQUEST_SETKINNOTIFICATION` | `0x00001100` | `0x00001795` | 577/271 | 5 | KinIndex@0:4, Uin@4:4, Time@8:4, NotificationLen@12:2, Notification@14:1[257]{count@12} |
| 88 | `RESPONSE_SETKINNOTIFICATION` | `0x00001100` | `0x00001796` | 578/783 | 7 | ResultID@0:2, KinIndex@2:4, Uin@6:4, NotificationLen@10:2, Notification@12:1[257]{count@10}, ReasonDesLen@269:2, ReasonDesStr@271:1[512]{count@269} |
| 89 | `REQUEST_FETCH_TOP` | `0x00001140` | `0x00001797` | 579/24 | 6 | Uin@0:4, Time@4:4, ClientOrderTime@8:4, KinIndex@12:4, HonorOrder@16:4, ActivePointOrder@20:4 |
| 90 | `RESPONSE_FETCH_TOP` | `0x00001100` | `0x00001798` | 580/1278 | 4 | ResultID@0:2, OrderTime@2:4, Uin@6:4, TopKinInfos@10:1268 |
| 91 | `REQUEST_SERVER` | `0x00001140` | `0x000003E9` | 206/8 | 2 | Uin@0:4, Time@4:4 |
| 92 | `RESPONSE_SERVER` | `0x00001100` | `0x000007D1` | 207/1203 | 3 | ResultID@0:2, ServerCount@2:1, Servers@3:12[100]{count@2} |
| 93 | `REQUEST_CHANNEL` | `0x00001140` | `0x000003EA` | 208/8 | 2 | Uin@0:4, Time@4:4 |
| 94 | `RESPONSE_CHANNEL` | `0x00001100` | `0x000007D2` | 209/176263 | 3 | ResultID@0:2, ChannelCount@2:1, Channels@3:17626[10]{count@2} |
| 95 | `REQUEST_CHANNEL_SECTION` | `0x00001100` | `0x000003EB` | 210/29 | 4 | Uin@0:4, Time@4:4, ChannelIDCount@8:1, ChannelIDs@9:2[10]{count@8} |
| 96 | `RESPONSE_CHANNEL_SECTION` | `0x00001100` | `0x000007D3` | 211/176033 | 3 | ResultID@0:2, ChannelSectionCount@2:1, ChannelSections@3:17603[10]{count@2} |
| 97 | `REQUEST_HALL` | `0x00001140` | `0x000003EC` | 212/12 | 3 | Uin@0:4, Time@4:4, Version@8:4 |
| 98 | `RESPONSE_HALL` | `0x00001100` | `0x000007D4` | 213/180032 | 10 | ResultID@0:2, AttachInfoLen@2:2, AttachInfo@4:1[512]{count@2}, KingdomStamp@516:4, ServerCount@520:1, Servers@521:12[100]{count@520}, ChannelCount@1721:1, Channels@1722:17626[10]{count@1721}, OtherAttachInfoLen@177982:2, OtherAttachInfo@177984:1[2048]{count@177982} |
| 99 | `REQUEST_KINGDOM` | `0x00001140` | `0x0000041E` | 302/8 | 2 | Uin@0:4, Time@4:4 |
| 100 | `DIR_SERVER_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/74 | 6 | ServerID@0:4, DNSNameLen@4:2, DNSName@6:1[40]{count@4}, ServerIP@46:4, ServerPortNum@50:4, ServerPorts@54:2[10]{count@50} |
| 101 | `KINGDOM_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/1570 | 6 | KingdomName@0:1[40], KingdomID@40:4, DNSNameLen@44:2, DNSName@46:1[40]{count@44}, DirNum@86:4, DirServers@90:74[20]{count@86} |
| 102 | `RESPONSE_KINGDOM` | `0x00001100` | `0x00000806` | 303/15710 | 4 | Result@0:2, KingdomStamp@2:4, KingdomNumber@6:4, Kingdoms@10:1570[10]{count@6} |
| 103 | `LOGINFILEINFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/20 | 3 | FileID@0:2, Version@2:2, FileHash@4:1[16] |
| 104 | `REQUEST_LOGIN` | `0x00001100` | `0x000003ED` | 214/453 | 14 | Uin@0:4, Time@4:4, QQNickname@8:1[20], Gender@28:1, IconID@29:1, PalDlg@30:2, SectionID@32:2, AttachIdentify@34:4, RoleID@38:1, ClientVersion@39:4, FileNum@43:2, CfgFileInfos@45:20[20]{count@43}, ClientType@445:4, CSVersion@449:4 |
| 105 | `RESPONSE_LOGIN` | `0x00001500` | `0x000007D5` | 215/9488 | 23 | ResultID@0:2, PlayerID@2:2, Uin@4:4, Identity@8:4, SectionID@12:2, KeyGameDataLength@14:1, KeyGameData@15:1[32], NumOfRoom@47:2, MinRoomID@49:2, GameInfo@51:47, ItemCount@98:2, Items@100:18[500]{count@98}, SectionMode@9100:1, ExtraSectionModeInfo@9101:4, SectionIdentity@9105:4, ReasonLen@9109:1, Reason@9110:1[200]{count@9109}, Honor@9310:4, TaskCount@9314:2, TaskGrade@9316:2, TaskGameCount@9318:2, TaskGameFinished@9320:2, PatternPoints@9322:166 |
| 106 | `REQUEST_SHOPLIST` | `0x00001140` | `0x00001799` | 581/20 | 5 | Uin@0:4, Time@4:4, ClientVersion@8:4, CSVersion@12:4, ShopListVersion@16:4 |
| 107 | `RESPONSE_SHOPLIST` | `0x00001100` | `0x0000179A` | 582/29019 | 8 | ResultID@0:2, DownloadResultID@2:2, LastestShopListVersion@4:4, RealFileLen@8:4, RealZipFileLen@12:4, BufferSequence@16:1, ThisTransLen@17:2, TransFileBuffer@19:1[29000]{count@17} |
| 108 | `REQUEST_LOGIN_DEAL` | `0x00001100` | `0x00000BBD` | 357/438 | 7 | Uin@0:4, Time@4:4, QQNickname@8:1[20], ClientVersion@28:4, FileNum@32:2, CfgFileInfos@34:20[20]{count@32}, CSVersion@434:4 |
| 109 | `RESPONSE_LOGIN_DEAL` | `0x00001100` | `0x00000BBE` | 358/246 | 8 | ResultID@0:2, PlayerID@2:2, Uin@4:4, Identity@8:4, KeyGameDataLength@12:1, KeyGameData@13:1[32], ReasonLen@45:1, Reason@46:1[200]{count@45} |
| 110 | `REQUEST_SALE_ITEM` | `0x00001140` | `0x00000BBF` | 359/44 | 11 | Uin@0:4, Time@4:4, SaleIndex@8:4, SaleType@12:4, SrcItemType@16:4, SrcItemID@20:4, SrcItemNum@24:4, DstUin@28:4, DstItemType@32:4, DstItemID@36:4, DstItemNum@40:4 |
| 111 | `RESPONSE_SALE_ITEM` | `0x00001100` | `0x00000BC0` | 360/239 | 12 | ResultID@0:2, SaleIndex@2:4, SaleType@6:4, SrcItemType@10:4, SrcItemID@14:4, SrcItemNum@18:4, DstUin@22:4, DstItemType@26:4, DstItemID@30:4, DstItemNum@34:4, ReasonLen@38:1, Reason@39:1[200]{count@38} |
| 112 | `GOODS_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 2 | ItemID@0:4, ItemNum@4:4 |
| 113 | `REQUEST_P2P_SALE_ITEM` | `0x00001100` | `0x00000BC2` | 362/120 | 8 | Uin@0:4, Time@4:4, SaleType@8:4, SrcItemKind@12:4, SrcGoods@16:8[6]{count@12}, DstUin@64:4, DstItemKind@68:4, DstGoods@72:8[6]{count@68} |
| 114 | `RESPONSE_P2P_SALE_ITEM` | `0x00001100` | `0x00000BC3` | 363/207 | 4 | ResultID@0:2, SaleType@2:4, ReasonLen@6:1, Reason@7:1[200]{count@6} |
| 115 | `REQUEST_TRANS_P2P_SALE_ITEM` | `0x00001100` | `0x00000BC4` | 364/1037 | 5 | Uin@0:4, Time@4:4, DstUin@8:4, BuffLen@12:1, TransBuffer@13:1[1024]{count@12} |
| 116 | `RESPONSE_TRANS_P2P_SALE_ITEM` | `0x00001100` | `0x00000BC5` | 365/203 | 3 | ResultID@0:2, ReasonLen@2:1, Reason@3:1[200]{count@2} |
| 117 | `NOTIFY_TRANS_P2P_SALE_ITEM` | `0x00001100` | `0x00000BC6` | 366/1029 | 3 | SrcUin@0:4, BuffLen@4:1, TransBuffer@5:1[1024]{count@4} |
| 118 | `REQUEST_START_SALE_ITEM` | `0x00001140` | `0x00000BC7` | 367/16 | 4 | Uin@0:4, Time@4:4, DstUin@8:4, SaleType@12:4 |
| 119 | `RESPONSE_START_SALE_ITEM` | `0x00001100` | `0x00000BC8` | 368/208 | 4 | ResultID@0:2, ReasonLen@2:2, Reason@4:1[200]{count@2}, SaleType@204:4 |
| 120 | `UIStatisticItem` | `0x00001140` | `0xFFFFFFFF` | 4294967295/5 | 2 | ItemID@0:1, OprtType@1:4 |
| 121 | `REQUEST_LOGOUT` | `0x00001100` | `0x000003EE` | 216/509 | 4 | Uin@0:4, Time@4:4, StatisticItemCount@8:1, StatisticItems@9:5[100]{count@8} |
| 122 | `RESPONSE_LOGOUT` | `0x00001140` | `0x000007D6` | 217/2 | 1 | ResultID@0:2 |
| 123 | `REQUEST_ROOM` | `0x00001140` | `0x000003EF` | 218/15 | 7 | Uin@0:4, Time@4:4, OprtType@8:1, StartRoomID@9:2, Number@11:2, GameMode@13:1, GameType@14:1 |
| 124 | `RESPONSE_ROOM` | `0x00001100` | `0x000007D7` | 219/12808 | 6 | ResultID@0:2, OprtType@2:1, RoomCount@3:2, Rooms@5:32[400]{count@3}, GameMode@12805:1, StartRoomID@12806:2 |
| 125 | `NOTIFY_ROOM` | `0x00001100` | `0x000003F0` | 220/12802 | 2 | RoomCount@0:2, Rooms@2:32[400]{count@0} |
| 126 | `ACK_ROOM` | `0x00001140` | `0x000007D8` | 221/2 | 1 | ResultID@0:2 |
| 127 | `REQUEST_PLAYER` | `0x00001140` | `0x000003F1` | 222/12 | 4 | Uin@0:4, Time@4:4, StartPlayerID@8:2, Number@10:2 |
| 128 | `RESPONSE_PLAYER_OLD` | `0x00001100` | `0x000007D9` | 223/40503 | 5 | ResultID@0:2, PlayerCount@2:1, Players@3:231[100]{count@2}, PlayersAttach@23103:8[100]{count@2}, PatternPoints@23903:166[100]{count@2} |
| 129 | `REQUEST_CREATE_ROOM` | `0x00001140` | `0x000003F2` | 224/50 | 7 | Uin@0:4, Time@4:4, RoomName@8:1[20], Flag@28:1, Password@29:1[16], GameType@45:1, ContinueID@46:4 |
| 130 | `RESPONSE_CREATE_ROOM` | `0x00001140` | `0x000007DA` | 225/4 | 2 | ResultID@0:2, RoomID@2:2 |
| 131 | `REQUEST_FIND_FRIEND` | `0x00001140` | `0x000003F3` | 226/12 | 3 | Uin@0:4, Time@4:4, FriendUin@8:4 |
| 132 | `RESPONSE_FIND_FRIEND` | `0x00001500` | `0x000007DB` | 227/9315 | 22 | ResultID@0:2, FriendUin@2:4, PlayerName@6:1[20], PlayerID@26:2, Gender@28:1, IconID@29:1, Identity@30:4, ServerID@34:4, DlgID@38:2, SectionID@40:2, RoomID@42:2, RoomName@44:1[20], Status@64:1, GameInfo@65:47, ItemCount@112:2, Items@114:18[500]{count@112}, KinIndex@9114:4, KinNameLen@9118:2, KinName@9120:1[17]{count@9118}, KinFlagID@9137:8, Honor@9145:4, PatternPoints@9149:166 |
| 133 | `REQUEST_CHAT_ACROSS_SECTION` | `0x00001100` | `0x000003F4` | 228/520 | 7 | Uin@0:4, Time@4:4, ServiceID@8:4, DestPlayerID@12:2, DestPlayerUin@14:4, ContentLen@18:2, Content@20:1[500]{count@18} |
| 134 | `RESPONSE_CHAT_ACROSS_SECTION` | `0x00001140` | `0x000007DC` | 229/6 | 2 | ResultID@0:2, DestPlayerUin@2:4 |
| 135 | `REQUEST_BOARDCAST` | `0x00001100` | `0x000003F5` | 230/516 | 6 | Uin@0:4, Time@4:4, DestPlayerID@8:2, DestPlayerUin@10:4, ContentLength@14:2, Content@16:1[500]{count@14} |
| 136 | `RESPONSE_BOARDCAST` | `0x00001140` | `0x000007DD` | 231/2 | 1 | ResultID@0:2 |
| 137 | `REQUEST_CHAT` | `0x00001100` | `0x000003F6` | 232/512 | 5 | Uin@0:4, Time@4:4, DestPlayerID@8:2, ContentLength@10:2, Content@12:1[500]{count@10} |
| 138 | `RESPONSE_CHAT` | `0x00001140` | `0x000007DE` | 233/2 | 1 | ResultID@0:2 |
| 139 | `REQUEST_ENTER_ROOM` | `0x00001140` | `0x000003F7` | 234/27 | 5 | Uin@0:4, Time@4:4, RoomID@8:2, RoleID@10:1, Password@11:1[16] |
| 140 | `RESPONSE_ENTER_ROOM_OLD` | `0x00001500` | `0x000007DF` | 235/74858 | 15 | ResultID@0:2, RoomID@2:2, TermAndSeat@4:1, MapID@5:2, RoomFlag@7:1, RoomOwnerID@8:2, PlayerCount@10:1, SeatStatus@11:1[8], Players@19:9172[8]{count@10}, PlayersAttach@73395:12[8]{count@10}, GameType@73491:1, BackgroundID@73492:4, PlayersSpouseUin@73496:4[8]{count@10}, WeddingModeID@73528:2, PatternPoints@73530:166[8]{count@10} |
| 141 | `NOTIFY_ENTER_ROOM_OLD` | `0x00001500` | `0x000003F8` | 236/9190 | 4 | PlayInfo@0:9172, RoomID@9172:2, PlayerInfoAttach@9174:12, SpouseUin@9186:4 |
| 142 | `SALE_ITEM_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/12 | 3 | Type@0:4, ItemID@4:4, ItemNum@8:4 |
| 143 | `NOTIFY_SALE_ITEM` | `0x00001100` | `0x00000BC1` | 361/248 | 5 | SaleItemType@0:4, CommitSaleItemNum@4:2, CommitSaleItemInfo@6:12[10]{count@4}, OnlineSaleItemNum@126:2, OnlineSaleItemInfo@128:12[10]{count@126} |
| 144 | `ACK_ENTER_ROOM` | `0x00001140` | `0x000007E0` | 237/2 | 1 | ResultID@0:2 |
| 145 | `REQUEST_LEAVE_ROOM` | `0x00001140` | `0x000003F9` | 238/8 | 2 | Uin@0:4, Time@4:4 |
| 146 | `RESPONSE_LEAVE_ROOM` | `0x00001140` | `0x000007E1` | 239/2 | 1 | ResultID@0:2 |
| 147 | `NOTIFY_LEAVE_ROOM` | `0x00001140` | `0x000003FA` | 240/8 | 4 | PlayerID@0:2, NewRoomOwnerID@2:2, NewArbiPlayerID@4:2, MapID@6:2 |
| 148 | `ACK_LEAVE_ROOM` | `0x00001140` | `0x000007E2` | 241/2 | 1 | ResultID@0:2 |
| 149 | `REQUEST_PLAY` | `0x00001100` | `0x000003FB` | 242/32778 | 4 | Uin@0:4, Time@4:4, GameDataLength@8:2, GameData@10:1[32768]{count@8} |
| 150 | `RESPONSE_PLAY` | `0x00001140` | `0x000007E3` | 243/2 | 1 | ResultID@0:2 |
| 151 | `NOTIFY_ITEM_UPDATE` | `0x00001140` | `0x000003FC` | 244/9 | 4 | ItemID@0:2, Number@2:2, ExpireDate@4:4, RoleID@8:1 |
| 152 | `ACK_ITEM_UPDATE` | `0x00001140` | `0x000007E4` | 245/2 | 1 | ResultID@0:2 |
| 153 | `NOTIFY_SECTION_MSG` | `0x00001100` | `0x000003FD` | 246/1054 | 9 | SrcPlayerID@0:2, DestPlayerID@2:2, MsgLength@4:2, Msg@6:1[1024]{count@4}, Uin@1030:4, Identity@1034:4, XeffectID@1038:4, Point@1042:4, KinFlagID@1046:8 |
| 154 | `ACK_SECTION_MSG` | `0x00001140` | `0x000007E5` | 247/2 | 1 | ResultID@0:2 |
| 155 | `NOTIFY_ROOM_MSG` | `0x00001100` | `0x000003FE` | 248/1030 | 4 | SrcPlayerID@0:2, DestPlayerID@2:2, MsgLength@4:2, Msg@6:1[1024]{count@4} |
| 156 | `ACK_ROOM_MSG` | `0x00001140` | `0x000007E6` | 249/2 | 1 | ResultID@0:2 |
| 157 | `REQUEST_KICKOFF_PLAYER` | `0x00001140` | `0x000003FF` | 250/14 | 4 | Uin@0:4, Time@4:4, PlayerID@8:2, PlayerUin@10:4 |
| 158 | `RESPONSE_KICKOFF_PLAYER` | `0x00001140` | `0x000007E7` | 251/2 | 1 | ResultID@0:2 |
| 159 | `REQUEST_CHANGE_TERM` | `0x00001140` | `0x00000400` | 252/10 | 3 | Uin@0:4, Time@4:4, TermID@8:2 |
| 160 | `RESPONSE_CHANGE_TERM` | `0x00001140` | `0x000007E8` | 253/2 | 1 | ResultID@0:2 |
| 161 | `REQUEST_CHANGE_ROLE` | `0x00001140` | `0x00000401` | 254/10 | 3 | Uin@0:4, Time@4:4, RoleID@8:2 |
| 162 | `RESPONSE_CHANGE_ROLE` | `0x00001140` | `0x000007E9` | 255/2 | 1 | ResultID@0:2 |
| 163 | `REQUEST_READY` | `0x00001140` | `0x00000402` | 256/8 | 2 | Uin@0:4, Time@4:4 |
| 164 | `RESPONSE_READY` | `0x00001140` | `0x000007EA` | 257/2 | 1 | ResultID@0:2 |
| 165 | `REQUEST_CANCEL_READY` | `0x00001140` | `0x00000403` | 258/8 | 2 | Uin@0:4, Time@4:4 |
| 166 | `RESPONSE_CANCEL_READY` | `0x00001140` | `0x000007EB` | 259/2 | 1 | ResultID@0:2 |
| 167 | `NOTIFY_KICKOFF_ROOM` | `0x00001100` | `0x00000404` | 260/209 | 5 | KickOffReasonID@0:2, RoomID@2:2, PlayerUin@4:4, AttachInfoLen@8:1, AttachInfo@9:1[200]{count@8} |
| 168 | `NOTIFY_KICKOFF_LOGIN` | `0x00001100` | `0x000007EC` | 261/207 | 4 | KickOffReasonID@0:2, PlayerUin@2:4, ReasonLen@6:1, ReasonStr@7:1[200]{count@6} |
| 169 | `NOTIFY_VOTE` | `0x00001140` | `0x00000405` | 262/6 | 2 | VoteSeq@0:4, VotePlayerID@4:2 |
| 170 | `ACK_VOTE` | `0x00001140` | `0x000007ED` | 263/6 | 2 | VoteSeq@0:4, VoteResult@4:2 |
| 171 | `NOTIFY_VOTE_RESULT` | `0x00001140` | `0x00000406` | 264/204 | 2 | VoteSeq@0:4, ResultID@4:1[200] |
| 172 | `REQUEST_CHANGE_MAP` | `0x00001140` | `0x00000407` | 265/15 | 5 | Uin@0:4, Time@4:4, NewMapID@8:2, GameType@10:1, ContinueID@11:4 |
| 173 | `RESPONSE_CHANGE_MAP` | `0x00001140` | `0x000007EF` | 266/2 | 1 | ResultID@0:2 |
| 174 | `NOTIFY_CHANGE_MAP` | `0x00001140` | `0x00000408` | 267/7 | 3 | NewMapID@0:2, GameType@2:1, ContinueID@3:4 |
| 175 | `ACK_CHANGE_MAP` | `0x00001140` | `0x000007F0` | 268/2 | 1 | ResultID@0:2 |
| 176 | `NOTIFY_CHANGE_TERM` | `0x00001140` | `0x00000409` | 269/7 | 3 | PlayerUin@0:4, PlayerID@4:2, NewTermID@6:1 |
| 177 | `ACK_CHANGE_TERM` | `0x00001140` | `0x000007F1` | 270/2 | 1 | ResultID@0:2 |
| 178 | `NOTIFY_CHANGE_ROLE` | `0x00001140` | `0x0000040A` | 271/7 | 3 | PlayerUin@0:4, PlayerID@4:2, NewRoleID@6:1 |
| 179 | `ACK_CHANGE_ROLE` | `0x00001140` | `0x000007F2` | 272/2 | 1 | ResultID@0:2 |
| 180 | `NOTIFY_PLAYER_READY_STATE` | `0x00001140` | `0x0000040B` | 273/7 | 3 | PlayerUin@0:4, PlayerID@4:2, PlayerReadyState@6:1 |
| 181 | `ACK_PLAYER_READY_STATE` | `0x00001140` | `0x000007F3` | 274/2 | 1 | ResultID@0:2 |
| 182 | `REQUEST_REQPAYFOR` | `0x00001100` | `0x0000042F` | 327/149 | 7 | Uin@0:4, Time@4:4, CommodityID@8:4, Dstuin@12:4, AttachInfoLen@16:1, AttachInfo@17:1[128]{count@16}, ClientVersion@145:4 |
| 183 | `RESPONSE_REQPAYFOR` | `0x00001140` | `0x00000817` | 328/130 | 2 | ResultID@0:2, ResultString@2:1[128] |
| 184 | `REQUEST_RESTORE` | `0x00001140` | `0x00000430` | 329/16 | 4 | Uin@0:4, Time@4:4, ItemID@8:4, ClientVersion@12:4 |
| 185 | `RESPONSE_RESTORE` | `0x00001140` | `0x00000818` | 330/136 | 4 | ResultID@0:2, ItemID@2:4, AvailPeriod@6:2, ResultString@8:1[128] |
| 186 | `REQUEST_LEAVEWORD` | `0x00001140` | `0x00000431` | 331/8 | 2 | Uin@0:4, Time@4:4 |
| 187 | `LEAVEWORDLIST` | `0x00001100` | `0xFFFFFFFF` | 4294967295/140 | 6 | WordID@0:1, WordType@1:1, WordTime@2:4, SrcUin@6:4, WordLen@10:2, Word@12:1[128]{count@10} |
| 188 | `RESPONSE_LEAVEWORD` | `0x00001100` | `0x00000819` | 332/7003 | 3 | ResultID@0:2, LeaveWordNum@2:1, WordList@3:140[50]{count@2} |
| 189 | `REQUST_DELETELEAVEWORD` | `0x00001100` | `0x00000432` | 333/59 | 4 | Uin@0:4, Time@4:4, DeleteWordNum@8:1, WordList@9:1[50]{count@8} |
| 190 | `NOTIFY_HAVINGSYSMSG` | `0x00001140` | `0x0000081C` | 336/4 | 1 | Uin@0:4 |
| 191 | `NOTIFY_PLAYER_ITEMADD` | `0x00001500` | `0x0000081D` | 337/378 | 6 | Uin@0:4, Time@4:4, SrcUin@8:4, CommodityID@12:4, ItemNum@16:2, Items@18:18[20]{count@16} |
| 192 | `REPORTFILEINFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/24 | 3 | FileID@0:4, FileVersion@4:4, FileHash@8:1[16] |
| 193 | `REQUEST_GETCFGFILE` | `0x00001100` | `0x0000081E` | 338/84 | 4 | Uin@0:4, Time@4:4, FileNum@8:4, CfgFileInfos@12:24[3]{count@8} |
| 194 | `RESPONSE_GETCFGFILE` | `0x00001100` | `0x0000081F` | 339/96052 | 2 | FileNum@0:4, CfgFileInfos@4:32016[3]{count@0} |
| 195 | `REQUEST_OPERATEITEM` | `0x00001100` | `0x00000820` | 340/142 | 7 | Uin@0:4, Time@4:4, OpID@8:4, CommodityID@12:4, ItemNum@16:2, ItemID@18:4[30]{count@16}, ClientVersion@138:4 |
| 196 | `RESPONSE_OPERATEITEM` | `0x00001100` | `0x00000821` | 341/262 | 7 | ResultID@0:2, OpID@2:4, CommodityID@6:4, ItemNum@10:2, ItemID@12:4[30]{count@10}, StringLen@132:2, ResultString@134:1[128]{count@132} |
| 197 | `REQUEST_DOTASK` | `0x00001140` | `0x00000822` | 342/20 | 5 | Uin@0:4, Time@4:4, OprID@8:4, TaskTime@12:4, TaskGrade@16:4 |
| 198 | `RESPONSE_DOTASK` | `0x00001100` | `0x00000823` | 343/224 | 8 | ResultID@0:2, Uin@2:4, OprID@6:4, TaskTime@10:4, TaskGrade@14:4, GameFinished@18:4, ReasonLen@22:2, Reason@24:1[200]{count@22} |
| 199 | `NOTIFY_TASKINFO` | `0x00001100` | `0x00000824` | 344/226 | 8 | Uin@0:4, OprID@4:4, TaskTime@8:4, TaskGrade@12:4, GameTotal@16:4, GameFinished@20:4, ReasonLen@24:2, Reason@26:1[200]{count@24} |
| 200 | `REQUEST_COMBINE_FORGE` | `0x00001100` | `0x00000825` | 345/99 | 7 | Uin@0:4, Time@4:4, Type@8:1, ItemID@9:4, FromItemID@13:4, MaterialNum@17:2, MaterialID@19:4[20]{count@17} |
| 201 | `REQUEST_FORGE` | `0x00001140` | `0x00000BBB` | 355/17 | 5 | Uin@0:4, Time@4:4, Type@8:1, ItemID@9:4, MaterialID@13:4 |
| 202 | `RESPONSE_COMBINE_FORGE` | `0x00001100` | `0x00000826` | 346/295 | 8 | ResultID@0:2, Type@2:1, ItemID@3:4, FromItemID@7:4, MaterialNum@11:2, MaterialID@13:4[20]{count@11}, ReasonLen@93:2, Reason@95:1[200]{count@93} |
| 203 | `RESPONSE_FORGE` | `0x00001100` | `0x00000BBC` | 356/215 | 8 | ResultID@0:2, Type@2:1, ItemID@3:4, MaterialID@7:4, Effect@11:1, Color@12:1, ReasonLen@13:2, Reason@15:1[200]{count@13} |
| 204 | `REQUEST_MODIFY_ROOMINFO` | `0x00001140` | `0x00000827` | 347/13 | 4 | Uin@0:4, Time@4:4, GameType@8:1, ContinueID@9:4 |
| 205 | `RESPONSE_MODIFY_ROOMINFO` | `0x00001100` | `0x00000828` | 348/203 | 3 | Result@0:2, ReasonLen@2:1, Reason@3:1[200]{count@2} |
| 206 | `NOTIYF_PLAYERRINFO_CHANGE` | `0x00001140` | `0x00000435` | 349/5 | 2 | GameType@0:1, ContinueID@1:4 |
| 207 | `CONTINUEFILE_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/10256 | 7 | FileID@0:4, Reserve@4:1, ResultID@5:1, Version@6:2, FileLen@8:4, FileZipLen@12:4, FileBuffer@16:1[10240]{count@12} |
| 208 | `REQUEST_CONTINUEFILE` | `0x00001100` | `0x0000082A` | 350/496 | 5 | Uin@0:4, Time@4:4, ContinueID@8:4, FileNum@12:4, FileInfo@16:24[20]{count@12} |
| 209 | `RESPONSE_GETCONTINUEFILE` | `0x00001100` | `0x0000082B` | 351/205128 | 3 | ContinueID@0:4, FileNum@4:4, FileInfos@8:10256[20]{count@4} |
| 210 | `REQUEST_COMMODITY_LIST` | `0x00001140` | `0x0000040C` | 275/8 | 2 | Uin@0:4, Time@4:4 |
| 211 | `REQUEST_COMMODITY_LIST_NEW` | `0x00001140` | `0x00000433` | 334/8 | 2 | Uin@0:4, Time@4:4 |
| 212 | `COMMODITY_ITEM` | `0x00001140` | `0xFFFFFFFF` | 4294967295/10 | 3 | ItemID@0:4, ItemNum@4:4, AvailPeriod@8:2 |
| 213 | `COMMODITY` | `0x00001100` | `0xFFFFFFFF` | 4294967295/510 | 11 | CommodityName@0:1[32], CommodityID@32:4, CommodityType@36:2, CommodityItemCount@38:2, CommodityItems@40:10[20]{count@38}, PriceQQ@240:4, PriceQQGame@244:4, PriceQQTang@248:4, Rebate@252:1, AttachInfoLen@253:1, AttachInfo@254:1[256]{count@253} |
| 214 | `RESPONSE_COMMODITY_LIST` | `0x00001100` | `0x000007F4` | 276/52033 | 6 | ResultID@0:2, EndFlag@2:1, CommodityNum@3:4, CommodityList@7:510[100]{count@3}, AttachInfoLen@51007:2, AttachInfo@51009:1[1024]{count@51007} |
| 215 | `COMMODITY_ITEM_NEW` | `0x00001140` | `0xFFFFFFFF` | 4294967295/10 | 3 | ItemID@0:4, ItemNum@4:4, AvailPeriod@8:2 |
| 216 | `COMMODITY_NEW` | `0x00001100` | `0xFFFFFFFF` | 4294967295/523 | 15 | CommodityName@0:1[32], CommodityID@32:4, CommodityType@36:2, CommodityItemCount@38:2, CommodityItems@40:10[20]{count@38}, PriceQQ@240:4, PriceQQGame@244:4, PriceQQTang@248:4, RestorPrice@252:4, SaleDateLimit@256:4, MemberRebate@260:1, Attribute@261:4, Rebate@265:1, AttachInfoLen@266:1, AttachInfo@267:1[256]{count@266} |
| 217 | `RESPONSE_COMMODITY_LIST_NEW` | `0x00001100` | `0x0000081B` | 335/262537 | 7 | ResultID@0:2, EndFlag@2:1, CommodityNum@3:4, CommodityListNew@7:523[500]{count@3}, RequestPayforCost@261507:4, AttachInfoLen@261511:2, AttachInfo@261513:1[1024]{count@261511} |
| 218 | `REQUEST_BUY` | `0x00001140` | `0x0000040D` | 277/26 | 8 | Uin@0:4, Time@4:4, CommodityID@8:4, DealType@12:2, PayType@14:2, ClientVersion@16:4, CommodityVersion@20:4, AgreeMixPay@24:2 |
| 219 | `RESPONSE_BUY` | `0x00001100` | `0x000007F5` | 278/268 | 9 | ResultID@0:2, ResultString@2:1[128], DealType@130:2, PayType@132:2, PayMoney@134:4, CommodityID@138:4, ItemNum@142:2, ItemID@144:4[30]{count@142}, TicketLeft@264:4 |
| 220 | `REQUEST_PRESENT` | `0x00001100` | `0x0000040E` | 279/283 | 10 | Uin@0:4, Time@4:4, CommodityID@8:4, DealType@12:2, PayType@14:2, DstUin@16:4, AttachInfoLen@20:1, AttachInfo@21:1[256]{count@20}, ClientVersion@277:4, AgreeMixPay@281:2 |
| 221 | `RESPONSE_PRESENT` | `0x00001140` | `0x000007F6` | 280/138 | 5 | ResultID@0:2, ResultString@2:1[128], DealType@130:2, PayType@132:2, PayMoney@134:4 |
| 222 | `REQUEST_HELLO` | `0x00001100` | `0x0000040F` | 281/42 | 4 | Uin@0:4, Time@4:4, InfoLength@8:2, Info@10:1[32]{count@8} |
| 223 | `RESPONSE_HELLO` | `0x00001140` | `0x00000410` | 282/2 | 1 | ResultID@0:2 |
| 224 | `REQUEST_TRANSFER_UDP_OK` | `0x00001100` | `0x00000411` | 283/48 | 6 | Uin@0:4, Time@4:4, DstDlg@8:2, DstUin@10:4, InfoLength@14:2, Info@16:1[32]{count@14} |
| 225 | `RESPONSE_TRANSFER_UDP_OK` | `0x00001140` | `0x00000412` | 284/2 | 1 | ResultID@0:2 |
| 226 | `NOTIFY_UDP_OK` | `0x00001100` | `0x00000413` | 285/48 | 6 | Uin@0:4, Time@4:4, SrcDlg@8:2, SrcUin@10:4, InfoLength@14:2, Info@16:1[32]{count@14} |
| 227 | `ITEM_CHANGE_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/6 | 3 | ItemID@0:4, NewStatus@4:1, NewRoleID@5:1 |
| 228 | `REQUEST_ITEM_STATUS_CHANGE` | `0x00001100` | `0x00000414` | 286/3010 | 4 | Uin@0:4, Time@4:4, ItemNum@8:2, Items@10:6[500]{count@8} |
| 229 | `RESPONSE_ITEM_STATUS_CHANGE` | `0x00001140` | `0x000007FC` | 287/2 | 1 | ResultID@0:2 |
| 230 | `REQUEST_USE_ITEM_IN_ROOM` | `0x00001140` | `0x00000415` | 288/12 | 3 | Uin@0:4, Time@4:4, ItemID@8:4 |
| 231 | `RESPONSE_USE_ITEM_IN_ROOM` | `0x00001140` | `0x000007FD` | 289/2 | 1 | ResultID@0:2 |
| 232 | `NOTIFY_USE_ITEM_IN_ROOM` | `0x00001140` | `0x00000416` | 290/10 | 3 | Uin@0:4, Dlg@4:2, ItemID@6:4 |
| 233 | `ROOM_PLAYER_MOVE_INFO` | `0x00001140` | `0x00000BB8` | 352/17 | 5 | SeatId@0:1, PosX@1:4, PosY@5:4, Dir@9:4, Speed@13:4 |
| 234 | `ROOM_PLAYER_PUT_BOMB` | `0x00001140` | `0x00000BB9` | 353/9 | 3 | SeatId@0:1, PosX@1:4, PosY@5:4 |
| 235 | `ROOM_MSG_DATA` | `0x00001100` | `0x00000BBA` | 354/812 | 5 | Time@0:4, DataID@4:2, DataLen@6:2, Sequence@8:4, Data@12:1[800]{count@6} |
| 236 | `REQUEST_SET_SEAT_STATUS` | `0x00001140` | `0x00000417` | 291/10 | 4 | Uin@0:4, Time@4:4, SeatID@8:1, NewStatus@9:1 |
| 237 | `RESPONSE_SET_SEAT_STATUS` | `0x00001140` | `0x000007FF` | 292/2 | 1 | ResultID@0:2 |
| 238 | `NOTIFY_SET_SEAT_STATUS` | `0x00001140` | `0x00000418` | 293/2 | 2 | SeatID@0:1, NewStatus@1:1 |
| 239 | `NOTIFY_TEST_NET_SPEED` | `0x00001140` | `0x00000419` | 294/8 | 2 | StartSec@0:4, StartUSec@4:4 |
| 240 | `ACK_TEST_NET_SPEED` | `0x00001140` | `0x00000801` | 295/8 | 2 | StartSec@0:4, StartUSec@4:4 |
| 241 | `REQUEST_START_GAME` | `0x00001140` | `0x0000041A` | 296/8 | 2 | Uin@0:4, Time@4:4 |
| 242 | `RESPONSE_START_GAME` | `0x00001140` | `0x00000802` | 297/2 | 1 | ResultID@0:2 |
| 243 | `NOTIFY_GAME_EVENT` | `0x00001100` | `0x0000041B` | 298/4106 | 4 | RoomID@0:2, GameDataSeq@2:4, GameDataLen@6:4, GameData@10:1[4096]{count@6} |
| 244 | `REQUEST_JOIN_ROOM` | `0x00001140` | `0x0000041D` | 300/9 | 3 | Uin@0:4, Time@4:4, GameType@8:1 |
| 245 | `RESPONSE_JOIN_ROOM_OLD` | `0x00001500` | `0x00000805` | 301/74876 | 15 | ResultID@0:2, RoomID@2:2, RoomName@4:1[20], TermAndSeat@24:1, MapID@25:2, RoomFlag@27:1, RoomOwnerID@28:2, PlayerCount@30:1, SeatStatus@31:1[8], Players@39:9172[8]{count@30}, PlayersAttach@73415:12[8]{count@30}, GameType@73511:1, BackgroundID@73512:4, PlayersSpouseUin@73516:4[8]{count@30}, PatternPoints@73548:166[8]{count@30} |
| 246 | `NOTIFY_GAME_MONEY` | `0x00001140` | `0x0000041F` | 304/11 | 4 | ResultID@0:2, MoneyType@2:1, Money@3:4, Uin@7:4 |
| 247 | `GAME_BEGIN_DATA` | `0x00001100` | `0x0000041C` | 299/1026 | 2 | DataLength@0:2, Data@2:1[1024]{count@0} |
| 248 | `NOTIFY_ACROSS_SECTION_MSG` | `0x00001100` | `0x00000420` | 305/538 | 4 | SrcUin@0:4, ContentLen@4:2, Content@6:1[500]{count@4}, Nickname@506:1[32] |
| 249 | `GAME_ITEM_TYPE` | `0x00001140` | `0xFFFFFFFF` | 4294967295/6 | 2 | ItemID@0:4, ItemCount@4:2 |
| 250 | `PLAYER_GAME_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/73 | 7 | PlayerID@0:2, RoleID@2:1, TeamID@3:1, DelayTime@4:4, ExtPoint@8:4, NewItemCount@12:1, NewItem@13:6[10]{count@12} |
| 251 | `BOSS_ITEM_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 3 | ItemID@0:4, ItemCount@4:2, DropTime@6:2 |
| 252 | `BOSS_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/446 | 19 | BossLable@0:4, BossID@4:2, RoleID@6:1, TeamID@7:1, AIType@8:4, Row@12:2, Col@14:2, HP@16:2, Rate@18:4, Bubble@22:4, Power@26:2, NormalItemCount@28:2, NormalItem@30:8[20]{count@28}, OutfitItemCount@190:2, OutfitItem@192:8[10]{count@190}, SkillCount@272:2, Skills@274:4[10]{count@272}, OtherLength@314:4, OtherContent@318:1[128]{count@314} |
| 253 | `PLAYER_EXT_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/16 | 4 | ExtWinNum@0:4, ExtLossNum@4:4, ExtEqualNum@8:4, ExtPoint@12:4 |
| 254 | `NOTIFY_GAME_BEGIN` | `0x00001100` | `0x00000FA1` | 426/1454 | 16 | GameID@0:4, MapID@4:4, SpawnSeed@8:4, ItemSeed@12:4, ArbitratePlayerID@16:2, PlayerNum@18:1, Players@19:73[8]{count@18}, ItemCount@603:1, Item@604:6[32]{count@603}, ReportFlag@796:1, MapHash@797:1[32], ItemNewCount@829:1, NewItem@830:6[100]{count@829}, ContinueID@1430:4, FileHash@1434:1[16], GameTime@1450:4 |
| 255 | `NOTIFY_GAME_NEXTMAP` | `0x00001100` | `0x00001177` | 458/1454 | 16 | GameID@0:4, MapID@4:4, SpawnSeed@8:4, ItemSeed@12:4, ArbitratePlayerID@16:2, PlayerNum@18:1, Players@19:73[8]{count@18}, ItemCount@603:1, Item@604:6[32]{count@603}, ReportFlag@796:1, MapHash@797:1[32], ItemNewCount@829:1, NewItem@830:6[100]{count@829}, ContinueID@1430:4, FileHash@1434:1[16], GameTime@1450:4 |
| 256 | `REQUEST_GAME_NEXTMAP` | `0x00001140` | `0x00001178` | 459/14 | 4 | PlayerID@0:2, Time@2:4, ContinueID@6:4, MapIndex@10:4 |
| 257 | `RESPONSE_GAME_NEXTMAP` | `0x00001100` | `0x00001179` | 460/203 | 3 | ResultID@0:2, ReasonLen@2:1, Reason@3:1[200]{count@2} |
| 258 | `QQT_MSG_DATA` | `0x00001100` | `0xFFFFFFFF` | 4294967295/822 | 7 | Time@0:4, DataID@4:4, DataLen@8:2, Sequence@10:4, Data@14:1[800]{count@8}, GameTime@814:4, Flag@818:4 |
| 259 | `QQT_DATA_PACKAGE` | `0x00001100` | `0x00000FBD` | 455/52875 | 6 | PlayerID@0:2, Time@2:4, GameID@6:4, MsgDataCount@10:1, MsgDataIndex@11:4[64]{count@10}, MsgDatas@267:822[64]{count@10} |
| 260 | `PLAYER_MOVE_COMPRESS` | `0x00001140` | `0x000010E9` | 470/9 | 4 | Time@0:4, PosX@4:2, PosY@6:2, DirState@8:1 |
| 261 | `PLAYER_MOVE_REV` | `0x00001140` | `0x000010EA` | 471/4 | 4 | DeltaTime@0:1, DeltaPosX@1:1, DeltaPosY@2:1, DirState@3:1 |
| 262 | `QQT_PACKAGE_TO_PLAYER` | `0x00001100` | `0x000010E7` | 469/6667 | 9 | GameID@0:2, PlayerID@2:2, FirstIndex@4:2, PlayerMoveCount@6:1, PlayerMoves@7:9[2]{count@6}, PlayerMoveRevCount@25:1, PlayerMoveRevs@26:4[16]{count@25}, MsgDataCount@90:1, MsgDatas@91:822[8]{count@90} |
| 263 | `QQT_PACKAGE_TO_SERVER` | `0x00001100` | `0x000010EB` | 472/209617 | 4 | PlayerID@0:2, Time@2:4, MsgDataCount@6:1, MsgDatas@7:822[255]{count@6} |
| 264 | `NOTIFY_PLAYER_LEAVE` | `0x00001140` | `0x000010EC` | 473/2 | 1 | PlayerID@0:2 |
| 265 | `PLAYER_MOVESEQ` | `0x00001140` | `0xFFFFFFFF` | 4294967295/20 | 10 | Seq@0:2, TimeStamp@2:4, CurPosX@6:2, CurPosY@8:2, ConerPosX@10:2, ConerPosY@12:2, EndPosX@14:2, EndPosY@16:2, WalkAndDir@18:1, Speed@19:1 |
| 266 | `PLAYER_MOVE` | `0x00001100` | `0x00000FA2` | 427/359 | 5 | PlayerID@0:2, Count@2:1, SeqReply@3:4, PlayerIDs@7:2[16]{count@2}, astMoveSeq@39:20[16]{count@2} |
| 267 | `PLAYER_MOVE_C` | `0x00001140` | `0x000010E6` | 468/4 | 4 | DeltaTime@0:1, DirAndState@1:1, DeltaX@2:1, DeltaY@3:1 |
| 268 | `PLAYER_USE_BOMB` | `0x00001140` | `0x00000FA3` | 428/15 | 7 | PlayerID@0:2, Time@2:4, BombID@6:4, Row@10:1, Col@11:1, BombPower@12:2, BombProp@14:1 |
| 269 | `NOTIFY_PLAYER_USE_BOMB` | `0x00001140` | `0x0000138B` | 429/15 | 7 | PlayerID@0:2, Time@2:4, BombID@6:4, Row@10:1, Col@11:1, BombPower@12:2, BombProp@14:1 |
| 270 | `EXPLODED_BOMB` | `0x00001140` | `0xFFFFFFFF` | 4294967295/16 | 9 | PlayerID@0:2, Time@2:4, BombID@6:4, Row@10:1, Col@11:1, RowMin@12:1, RowMax@13:1, ColMin@14:1, ColMax@15:1 |
| 271 | `EXPLODED_BOMB_C` | `0x00001140` | `0xFFFFFFFF` | 4294967295/13 | 9 | PlayerID@0:2, Time@2:4, BombID@6:1, Row@7:1, Col@8:1, RowMin@9:1, RowMax@10:1, ColMin@11:1, ColMax@12:1 |
| 272 | `EXPLODED_MAPELEM_C` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 4 | MapElemID@0:2, UniqueID@2:4, Row@6:1, Col@7:1 |
| 273 | `EXPLODED_ITEM_C` | `0x00001140` | `0xFFFFFFFF` | 4294967295/6 | 3 | ItemID@0:4, Row@4:1, Col@5:1 |
| 274 | `EXPLODED_MAPELEM` | `0x00001140` | `0xFFFFFFFF` | 4294967295/4 | 3 | MapElemID@0:2, Row@2:1, Col@3:1 |
| 275 | `NOTIFY_BOMB_EXPLODE` | `0x00001100` | `0x00000FA4` | 430/1161 | 8 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bombs@7:13[64]{count@6}, MapElemCount@839:1, MapElems@840:4[32]{count@839}, ItemCount@968:1, Items@969:6[32]{count@968} |
| 276 | `NOTIFY_MAPELEM_BEEXPLODED` | `0x00001100` | `0x00000FBE` | 456/263 | 4 | PlayerID@0:2, Time@2:4, MapElemCount@6:1, MapElems@7:8[32]{count@6} |
| 277 | `EXPLODED_ITEM` | `0x00001140` | `0xFFFFFFFF` | 4294967295/6 | 3 | ItemID@0:4, Row@4:1, Col@5:1 |
| 278 | `NOTIFY_ITEM_BEEXPLODED` | `0x00001100` | `0x00000FBF` | 457/199 | 4 | PlayerID@0:2, Time@2:4, ItemCount@6:1, Items@7:6[32]{count@6} |
| 279 | `PLAYER_BE_EXPLODED` | `0x00001140` | `0x00000FA5` | 431/11 | 5 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, IsAvatar@10:1 |
| 280 | `NOTIFY_PLAYER_BE_EXPLODED` | `0x00001140` | `0x00000FA6` | 432/11 | 5 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, IsAvatar@10:1 |
| 281 | `QQT_GAME_ITEM` | `0x00001140` | `0xFFFFFFFF` | 4294967295/6 | 3 | ItemID@0:4, Row@4:1, Col@5:1 |
| 282 | `NOTIFY_PLAYER_DIE` | `0x00001100` | `0x00000FA7` | 433/395 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, ItemCount@10:1, Items@11:6[64]{count@10} |
| 283 | `NOTIFY_NPCUSE_SKILL` | `0x00001100` | `0x00001169` | 510/397 | 7 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, SkillID@10:2, ItemCount@12:1, Items@13:6[64]{count@12} |
| 284 | `NOTIFY_NPC_TALK` | `0x00001100` | `0x0000116A` | 511/516 | 3 | PlayerID@0:2, WordLength@2:2, Words@4:1[512]{count@2} |
| 285 | `NOTIFY_NPC_DROPITEM` | `0x00001100` | `0x0000116B` | 512/391 | 5 | PlayerID@0:2, PosX@2:2, PosY@4:2, ItemCount@6:1, Items@7:6[64]{count@6} |
| 286 | `REQUST_IN_TREASURE` | `0x00001140` | `0x0000116C` | 513/8 | 2 | Uin@0:4, Time@4:4 |
| 287 | `RESPONSE_IN_TREASURE` | `0x00001500` | `0x0000116D` | 514/548 | 4 | ResultID@0:2, MapID@2:4, ItemNum@6:2, Item@8:18[30]{count@6} |
| 288 | `REQUST_GETITEM_TREASURE` | `0x00001140` | `0x0000116E` | 515/12 | 3 | Uin@0:4, Time@4:4, ItemID@8:4 |
| 289 | `RESPONSE_GETITEM_TREASURE` | `0x00001140` | `0x0000116F` | 516/2 | 1 | ResultID@0:2 |
| 290 | `NOTIFY_LEAVE_TREASURE` | `0x00001140` | `0x00001170` | 517/8 | 2 | Uin@0:4, Time@4:4 |
| 291 | `NOTIFY_TREASURE_TIMEOUT` | `0x00001140` | `0x00001171` | 518/8 | 2 | Uin@0:4, Time@4:4 |
| 292 | `NOTIFY_CHECK_GAMETIME` | `0x00001140` | `0x00001172` | 519/4 | 1 | CheckOpID@0:4 |
| 293 | `NOTIFY_GAMETIME` | `0x00001140` | `0x00001173` | 520/10 | 3 | PlayerID@0:2, Uin@2:4, GameTime@6:4 |
| 294 | `REQUEST_PREPARED_USE_PROP` | `0x00001140` | `0x00001174` | 521/26 | 8 | PlayerID@0:2, ItemID@2:4, PosX@6:2, PoxY@8:2, Flag1@10:4, Flag2@14:4, Flag3@18:4, Flag4@22:4 |
| 295 | `NOTIFY_PREPARED_USE_PROP` | `0x00001140` | `0x00001175` | 522/26 | 8 | PlayerID@0:2, ItemID@2:4, PosX@6:2, PoxY@8:2, Flag1@10:4, Flag2@14:4, Flag3@18:4, Flag4@22:4 |
| 296 | `NOTIFY_EVENT_MAP_ELEM` | `0x00001140` | `0x00001176` | 523/16 | 7 | EventID@0:2, PlayerID@2:2, Time@4:4, SrcRow@8:2, SrcCol@10:2, DestRow@12:2, DestCol@14:2 |
| 297 | `NOTIFY_USE_EMOTION` | `0x00001140` | `0x00001194` | 524/12 | 3 | Uin@0:4, Time@4:4, EmotionID@8:4 |
| 298 | `REQUEST_KILL_PLAYER` | `0x00001140` | `0x00000FA8` | 434/12 | 5 | PlayerID@0:2, Time@2:4, DestPlayerID@6:2, PosX@8:2, PosY@10:2 |
| 299 | `NOTIFY_PLAYER_BE_KILLED` | `0x00001100` | `0x00000FA9` | 435/397 | 7 | PlayerID@0:2, Time@2:4, DestPlayerID@6:2, PosX@8:2, PosY@10:2, ItemCount@12:1, Items@13:6[64]{count@12} |
| 300 | `REQUEST_SAVE_PLAYER` | `0x00001140` | `0x00000FAA` | 436/12 | 5 | PlayerID@0:2, Time@2:4, DestPlayerID@6:2, PosX@8:2, PosY@10:2 |
| 301 | `NOTIFY_PLAYER_BE_SAVED` | `0x00001140` | `0x00000FAB` | 437/12 | 5 | PlayerID@0:2, Time@2:4, DestPlayerID@6:2, PosX@8:2, PosY@10:2 |
| 302 | `REQUEST_GET_ITEM` | `0x00001140` | `0x00000FAC` | 438/14 | 5 | PlayerID@0:2, Time@2:4, ItemID@6:4, PosX@10:2, PosY@12:2 |
| 303 | `NOTIFY_PLAYER_GET_ITEM` | `0x00001140` | `0x00000FAD` | 439/14 | 5 | PlayerID@0:2, Time@2:4, ItemID@6:4, PosX@10:2, PosY@12:2 |
| 304 | `ITEM_FROM_SERVER` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 2 | ItemID@0:4, DispatchTime@4:4 |
| 305 | `NOTIFY_DISPATCH_ITEM` | `0x00001100` | `0x00000FAE` | 440/710 | 7 | Time@0:4, ItemCount@4:1, Items@5:6[64]{count@4}, ItemFromServerCount@389:1, ItemFromServer@390:8[32]{count@389}, ItemFromServerRow@646:1[32]{count@389}, ItemFromServerCol@678:1[32]{count@389} |
| 306 | `REQUEST_USE_ITEM` | `0x00001140` | `0x00000FAF` | 441/30 | 9 | PlayerID@0:2, Time@2:4, ItemID@6:4, PosX@10:2, PosY@12:2, Flag1@14:4, Flag2@18:4, Flag3@22:4, Flag4@26:4 |
| 307 | `NOTIFY_PLAYER_USE_ITEM` | `0x00001140` | `0x00000FB0` | 442/30 | 9 | PlayerID@0:2, Time@2:4, ItemID@6:4, PosX@10:2, PosY@12:2, Flag1@14:4, Flag2@18:4, Flag3@22:4, Flag4@26:4 |
| 308 | `NOTIFY_PLAYER_USE_AFFECTION` | `0x00001140` | `0x00000FB5` | 443/30 | 9 | PlayerID@0:2, Time@2:4, ItemID@6:4, PosX@10:2, PosY@12:2, Flag1@14:4, Flag2@18:4, Flag3@22:4, Flag4@26:4 |
| 309 | `NOTIFY_CHANGE_ARBI_PLAYER` | `0x00001140` | `0x00000FB1` | 444/6 | 2 | ArbiPlayerID@0:2, Time@2:4 |
| 310 | `REQUEST_MOVE_MAPELEM` | `0x00001140` | `0x00000FB2` | 445/15 | 7 | PlayerID@0:2, Time@2:4, MapElemID@6:4, Row@10:1, Col@11:1, Dir@12:1, Speed@13:2 |
| 311 | `NOTIFY_PLAYER_MOVE_MAPELEM` | `0x00001140` | `0x00000FB3` | 446/17 | 7 | PlayerID@0:2, Time@2:4, MapElemID@6:4, Row@10:2, Col@12:2, Dir@14:1, Speed@15:2 |
| 312 | `REQUEST_MOVE_BOMB` | `0x00001140` | `0x00000FB4` | 447/20 | 10 | PlayerID@0:2, Time@2:2, FromRow@4:2, FromCol@6:2, ToRow@8:2, ToCol@10:2, Dir@12:1, BombPlayerID@13:2, BombTime@15:4, BombPower@19:1 |
| 313 | `NOTIFY_PLAYER_MOVE_BOMB` | `0x00001140` | `0x0000139C` | 448/20 | 10 | PlayerID@0:2, Time@2:2, FromRow@4:2, FromCol@6:2, ToRow@8:2, ToCol@10:2, Dir@12:1, BombPlayerID@13:2, BombTime@15:4, BombPower@19:1 |
| 314 | `REQUEST_GET_BUN` | `0x00001140` | `0x00000FB6` | 449/12 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, BunID@10:1, BunTeamID@11:1 |
| 315 | `NOTIFY_PLAYER_GET_BUN` | `0x00001140` | `0x00000FB7` | 450/12 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, BunID@10:1, BunTeamID@11:1 |
| 316 | `REQUEST_PUT_BUN` | `0x00001140` | `0x00000FB8` | 451/12 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, BunID@10:1, BunTeamID@11:1 |
| 317 | `NOTIFY_PLAYER_PUT_BUN` | `0x00001140` | `0x00000FB9` | 452/12 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, BunID@10:1, BunTeamID@11:1 |
| 318 | `NOTIFY_PLAYER_RELIVE` | `0x00001140` | `0x00000FBA` | 453/8 | 4 | PlayerID@0:2, Time@2:4, Row@6:1, Col@7:1 |
| 319 | `NOTIFY_RECOVER_PLAYER_AVATAR` | `0x00001140` | `0x000010E0` | 462/10 | 4 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2 |
| 320 | `REQUEST_TANKBASE_HPCHANGE` | `0x00001140` | `0x000015B3` | 528/10 | 4 | PlayerID@0:2, Time@2:4, DestTeamID@6:2, ChangeHP@8:2 |
| 321 | `QQT_PLAYER_RESULT_DATA` | `0x00001100` | `0xFFFFFFFF` | 4294967295/82 | 14 | PlayerID@0:2, Uin@2:4, SeatID@6:1, Nickname@7:1[20], Gender@27:1, Result@28:1, Remark@29:4, Point@33:4, Rating@37:4, StartExperience@41:4, EndExperience@45:4, FieldCount@49:1, FieldValue1@50:4[4]{count@49}, FieldValue2@66:4[4]{count@49} |
| 322 | `QQT_ALL_PLAYER_RESULT_DATA` | `0x00001140` | `0xFFFFFFFF` | 4294967295/659 | 3 | LocalPlayerID@0:2, PlayerCount@2:1, PlayerResultData@3:82[8] |
| 323 | `QQT_GAME_RESULT_DATA` | `0x00001100` | `0xFFFFFFFF` | 4294967295/44 | 7 | PlayerID@0:2, Result@2:1, Remark@3:4, Point@7:4, FieldCount@11:1, FieldValue1@12:4[4]{count@11}, FieldValue2@28:4[4]{count@11} |
| 324 | `NOTIFY_GAME_OVER` | `0x00001100` | `0x00000FBB` | 454/358 | 4 | Time@0:4, ResultDataCount@4:1, ResultData@5:44[8]{count@4}, GameMode@357:1 |
| 325 | `QQT_GAME_BOMB` | `0x00001140` | `0xFFFFFFFF` | 4294967295/7 | 4 | BombID@0:4, BombPower@4:1, Row@5:1, Col@6:1 |
| 326 | `REQUEST_LAUNCH_MACHINEBOMB` | `0x00001140` | `0x000010EF` | 476/8 | 4 | FirstTeam@0:1, RoleID@1:1, PlayerID@2:2, Time@4:4 |
| 327 | `NOTIFY_MACHINE_BOMB_FREE_S` | `0x00001100` | `0x000010F0` | 477/77 | 4 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bombs@7:7[10]{count@6} |
| 328 | `NOTIFY_MACHINEBOMB_MOVE` | `0x00001100` | `0x0000115D` | 498/77 | 4 | PlayerID@0:2, Time@2:4, BombNum@6:1, astBomb@7:7[10]{count@6} |
| 329 | `REQUEST_PUSH_MAPELEM` | `0x00001100` | `0x0000115E` | 499/16 | 5 | PlayerID@0:2, Dir@2:1, Power@3:1, Time@4:4, MapElem@8:8 |
| 330 | `NOTIFY_GENERATE_BOX` | `0x00001100` | `0x00001168` | 509/161 | 2 | MapElemCounts@0:1, MapElems@1:8[20]{count@0} |
| 331 | `NOTIFY_PUSH_MAPELEM` | `0x00001100` | `0x0000115F` | 500/26 | 9 | PlayerID@0:2, Dir@2:1, Power@3:1, DestRow@4:1, DestCol@5:1, BoxCurrentPosX@6:4, BoxCurrentPosY@10:4, Time@14:4, MapElem@18:8 |
| 332 | `NOTIFY_MAPELEM_KNOCK` | `0x00001100` | `0x00001160` | 501/261 | 4 | Time@0:4, PairCount@4:1, MapElem1@5:8[16]{count@4}, MapElem2@133:8[16]{count@4} |
| 333 | `BOX` | `0x00001140` | `0xFFFFFFFF` | 4294967295/12 | 6 | Row@0:1, Col@1:1, StopRow@2:1, StopCol@3:1, MapElemID@4:4, UniqueID@8:4 |
| 334 | `NOTIFY_MAPELEM_STOP` | `0x00001100` | `0x00001161` | 502/389 | 3 | MapElemCount@0:1, Time@1:4, MapElemID@5:12[32] |
| 335 | `NOTIFY_PLAYER_BE_KNOCKED` | `0x00001140` | `0x00001162` | 503/11 | 5 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, IsAvatar@10:1 |
| 336 | `REQUEST_DESTROY_BOX` | `0x00001100` | `0x00001163` | 504/18 | 5 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, MapElem@10:8 |
| 337 | `PLAYER_BE_KNOCKED` | `0x00001140` | `0x00001164` | 505/11 | 5 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, IsAvatar@10:1 |
| 338 | `BOX_BESTOPPED` | `0x00001100` | `0x00001165` | 506/391 | 4 | PlayerID@0:2, Time@2:4, BoxCount@6:1, Box@7:12[32]{count@6} |
| 339 | `NOTIFY_PLAYER_BETRAPPED` | `0x00001140` | `0x00001166` | 507/6 | 2 | PlayerID@0:2, Time@2:4 |
| 340 | `NOTIFY_FIRE` | `0x00001140` | `0x00001167` | 508/7 | 3 | PlayerID@0:2, Time@2:4, Fire@6:1 |
| 341 | `CREATE_NPC_BOSS` | `0x00001100` | `0x00001389` | 525/3576 | 3 | BossNum@0:4, Bosses@4:446[8]{count@0}, Time@3572:4 |
| 342 | `PVEBOSS_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/85 | 4 | BossID@0:2, BossNum@2:1, NormalItemCount@3:2, NormalItem@5:8[10]{count@3} |
| 343 | `CREATE_PVENPC_BOSS` | `0x00001100` | `0x0000138A` | 526/3404 | 2 | BossNum@0:4, bosses@4:85[40]{count@0} |
| 344 | `PVENPC` | `0x00001140` | `0xFFFFFFFF` | 4294967295/72 | 14 | NPCID@0:4, NPCBaseID@4:4, IsBoss@8:4, NickName@12:1[20], ImageID@32:4, MoveStrategy@36:4, Defense@40:4, HP@44:4, Speed@48:4, Harm@52:4, BattleStrategy@56:4, BornRow@60:4, BornCol@64:4, View@68:4 |
| 345 | `NOTIFY_CREATE_PVENPC` | `0x00001100` | `0x0000138C` | 527/2884 | 2 | BossNum@0:4, bosses@4:72[40]{count@0} |
| 346 | `NOTIFY_MACHINE_BOMB_ANGRY_D` | `0x00001100` | `0x000010F1` | 478/14 | 4 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bomb@7:7 |
| 347 | `NOTIFY_MACHINE_BOMB_GRACE_X` | `0x00001100` | `0x000010F2` | 479/70 | 4 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bombs@7:7[9]{count@6} |
| 348 | `NOTIFY_MACHINE_BOMB_COOL_Q` | `0x00001100` | `0x000010F3` | 480/14 | 4 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bomb@7:7 |
| 349 | `PLAYER_BE_HARMED` | `0x00001140` | `0x000010F4` | 481/13 | 6 | PlayerID@0:2, Time@2:4, PosX@6:2, PosY@8:2, IsAvatar@10:1, LossHP@11:2 |
| 350 | `REQUEST_CANCEL_USE_PROP` | `0x00001140` | `0x000010F5` | 482/10 | 3 | PlayerID@0:2, PropID@2:4, Time@6:4 |
| 351 | `NOTIFY_CANCEL_USE_PROP` | `0x00001140` | `0x000010F6` | 483/10 | 3 | PlayerID@0:2, PropID@2:4, Time@6:4 |
| 352 | `AFFECTED_PLAYER_INFO` | `0x00001140` | `0xFFFFFFFF` | 4294967295/11 | 4 | PlayerID@0:2, PosX@2:4, PosY@6:4, Dir@10:1 |
| 353 | `NOTIFY_PROP_TRIGGER` | `0x00001100` | `0x000010F7` | 484/134 | 9 | PropGUID@0:4, GameTime@4:4, UserID@8:2, PosX@10:4, PosY@14:4, Dir@18:1, PlayerNum@19:1, AffectedPlayers@20:11[10]{count@19}, TypeID@130:4 |
| 354 | `NOTIFY_PRODUCE_ITEM` | `0x00001140` | `0x0000115A` | 495/5 | 2 | RowAndCol@0:1, ItemID@1:4 |
| 355 | `NOTIFY_WRESTLE_SKILL` | `0x00001140` | `0x0000115B` | 496/9 | 5 | SkillType@0:1, FirstTeam@1:1, PlayerPos@2:1, PlayerID@3:2, Time@5:4 |
| 356 | `REQUEST_WRESTLE` | `0x00001140` | `0x0000115C` | 497/9 | 5 | SkillType@0:1, FirstTeam@1:1, PlayerPos@2:1, PlayerID@3:2, Time@5:4 |
| 357 | `PLAYER_THROW_BOMB` | `0x00001140` | `0x00001159` | 494/13 | 8 | BombRowAndCol@0:1, BombDestRowAndCol@1:1, BombProp@2:1, Dir@3:1, Power@4:1, Time@5:4, PlayerID@9:2, BombPower@11:2 |
| 358 | `NOTIFY_DISPATCH_BOMB` | `0x00001100` | `0x000010E1` | 463/616 | 6 | PlayerID@0:2, Time@2:4, BombCount@6:1, Bombs@7:7[32]{count@6}, ItemCount@231:1, Items@232:6[64]{count@231} |
| 359 | `REQUEST_EAT_BOMB` | `0x00001140` | `0x000010E2` | 464/20 | 9 | PlayerID@0:2, Time@2:2, PlayerAvatar@4:4, PosX@8:2, PosY@10:2, BombPlayerID@12:2, BombTime@14:4, BombRow@18:1, BombCol@19:1 |
| 360 | `NOTIFY_PLAYER_EAT_BOMB` | `0x00001140` | `0x000010E3` | 465/20 | 9 | PlayerID@0:2, Time@2:2, PlayerAvatar@4:4, PosX@8:2, PosY@10:2, BombPlayerID@12:2, BombTime@14:4, BombRow@18:1, BombCol@19:1 |
| 361 | `ACK_RECV_DATA` | `0x00001140` | `0x000010E4` | 466/6 | 2 | PlayerID@0:2, Time@2:4 |
| 362 | `REQUEST_GET_GAME_MONEY` | `0x00001140` | `0x00000421` | 306/8 | 2 | Uin@0:4, Time@4:4 |
| 363 | `RESPONSE_GET_GAME_MONEY` | `0x00001140` | `0x00000809` | 307/10 | 3 | ResultID@0:2, Money@2:4, Uin@6:4 |
| 364 | `CHECK_ONLY_DATA` | `0x00001100` | `0x000010E5` | 467/1032 | 4 | Time@0:4, DataID@4:2, DataLen@6:2, Data@8:1[1024]{count@6} |
| 365 | `NOTIFY_SYS_MSG` | `0x00001100` | `0x00001F41` | 529/2051 | 3 | SysMsgType@0:1, BCContentLen@1:2, BCContent@3:1[2048]{count@1} |
| 366 | `REQUEST_KICK` | `0x00001100` | `0x00001F42` | 530/1442 | 7 | Uin@0:4, Time@4:4, KickKind@8:4, TargetObjNum@12:4, TargetObj@16:4[100]{count@12}, AttachInfoLen@416:2, AttachInfo@418:1[1024]{count@416} |
| 367 | `RESPONSE_KICK` | `0x00001140` | `0x00001F43` | 531/2 | 1 | ResultID@0:2 |
| 368 | `NOTIYF_KICK_BY_GM` | `0x00001100` | `0x00001F44` | 532/1034 | 4 | GMUin@0:4, KickKind@4:4, AttachInfoLen@8:2, AttachInfo@10:1[1024]{count@8} |
| 369 | `REQUEST_FORBIDDEN` | `0x00001100` | `0x00001F45` | 533/1446 | 8 | Uin@0:4, Time@4:4, ForbiddenKind@8:4, ForbiddenPeriod@12:4, TargetObjNum@16:4, TargetObj@20:4[100]{count@16}, AttachInfoLen@420:2, AttachInfo@422:1[1024]{count@420} |
| 370 | `RESPONSE_FORBIDDEN` | `0x00001140` | `0x00001F46` | 534/2 | 1 | ResultID@0:2 |
| 371 | `NOTIFY_FORBIDDEN` | `0x00001100` | `0x00001F47` | 535/1038 | 5 | GMUin@0:4, ForbiddenKind@4:4, ForbiddenPeriod@8:4, AttachInfoLen@12:2, AttachInfo@14:1[1024]{count@12} |
| 372 | `REQUEST_DISMISS_GAME` | `0x00001100` | `0x00001F48` | 536/1036 | 5 | Uin@0:4, Time@4:4, RoomID@8:2, AttachInfoLen@10:2, AttachInfo@12:1[1024]{count@10} |
| 373 | `RESPONSE_DISMISS_GAME` | `0x00001140` | `0x00001F49` | 537/2 | 1 | ResultID@0:2 |
| 374 | `NOTIFY_DISMISS_GAME` | `0x00001100` | `0x00001F4A` | 538/1030 | 3 | GMUin@0:4, AttachInfoLen@4:2, AttachInfo@6:1[1024]{count@4} |
| 375 | `REPORT_CHEAT` | `0x00001100` | `0x00001F54` | 539/518 | 4 | CheatPlayerID@0:2, CheatReason@2:2, AttachInfoLen@4:2, AttachInfo@6:1[512]{count@4} |
| 376 | `REQUST_SET_PLAYER_IDENTITY` | `0x00001140` | `0x00001F55` | 540/12 | 3 | Uin@0:4, Time@4:4, Identity@8:4 |
| 377 | `RESPONSE_SET_PLAYER_IDENTITY` | `0x00001140` | `0x00001F56` | 541/10 | 3 | ResultID@0:2, Uin@2:4, Identity@6:4 |
| 378 | `REQUEST_REP_USE_CRIBBER` | `0x00001140` | `0x00000422` | 308/12 | 3 | Uin@0:4, Time@4:4, CharacterID@8:4 |
| 379 | `RESPONSE_REP_USE_CRIBBER` | `0x00001140` | `0x0000080A` | 309/2 | 1 | ResultID@0:2 |
| 380 | `REQUEST_TRAIN_HORTATION` | `0x00001140` | `0x00000423` | 310/20 | 6 | Uin@0:4, Time@4:4, Money@8:4, SecsInGame@12:4, TrainPhaseFlag@16:2, TrainType@18:2 |
| 381 | `REQUEST_MODIFY_ROOM` | `0x00001140` | `0x00000424` | 311/54 | 8 | Uin@0:4, Time@4:4, ModifyFlag@8:4, RoomName@12:1[20], RoomFlag@32:1, PassWord@33:1[16], GameType@49:1, ContinueID@50:4 |
| 382 | `RESPONSE_MODIFY_ROOM` | `0x00001140` | `0x0000080C` | 312/2 | 1 | Result@0:2 |
| 383 | `NOTIYF_PLAYER_ROOM_CHANGE` | `0x00001140` | `0x00000425` | 313/41 | 4 | ModifyFlag@0:4, RoomName@4:1[20], RoomFlag@24:1, PassWord@25:1[16] |
| 384 | `REQUEST_ADVISE` | `0x00001100` | `0x00000426` | 314/1038 | 5 | Uin@0:4, Time@4:4, AdviseType@8:4, AdviseLen@12:2, AdviseContent@14:1[1024]{count@12} |
| 385 | `NOTIFY_POINTS_REFRESH` | `0x00001140` | `0x00000427` | 315/226 | 16 | Uin@0:4, Time@4:4, WinNum@8:4, LossNum@12:4, EqualNum@16:4, OrgID@20:4, Point@24:4, PetID@28:4, Money@32:4, Identify@36:4, ExtWinNum@40:4, ExtLossNum@44:4, ExtEqualNum@48:4, ExtPoint@52:4, Honor@56:4, PatternPoints@60:166 |
| 386 | `REQUEST_TCP_TRANSFER` | `0x00001100` | `0x00000428` | 316/1804 | 4 | Uin@0:4, Time@4:4, DataLen@8:4, Data@12:1[1792]{count@8} |
| 387 | `RESPONSE_TCP_TRANSFER` | `0x00001140` | `0x00000810` | 317/2 | 1 | Result@0:2 |
| 388 | `NOTIFY_TCP_TRANSFER` | `0x00001100` | `0x00000429` | 318/1806 | 5 | Uin@0:4, Time@4:4, PlayerID@8:2, DataLen@10:4, Data@14:1[1792]{count@10} |
| 389 | `NOTIFY_PLAYER_STATUS_OLD` | `0x00001500` | `0x0000042B` | 320/9249 | 6 | Uin@0:4, Status@4:4, PlayerInfo@8:231, ItemCount@239:2, Items@241:18[500]{count@239}, PlayerInfoAttach@9241:8 |
| 390 | `REQUEST_CANCEL_TEAM` | `0x00001140` | `0x0000042C` | 321/12 | 3 | Uin@0:4, Time@4:4, CancelUin@8:4 |
| 391 | `RESPONSE_CANCEL_TEAM` | `0x00001140` | `0x00000814` | 322/2 | 1 | ResultID@0:2 |
| 392 | `REQUEST_INVITE_TEAM` | `0x00001100` | `0x0000042D` | 323/33 | 4 | Uin@0:4, Time@4:4, PlayerCount@8:1, InviteUin@9:4[6]{count@8} |
| 393 | `RESPONSE_INVITE_TEAM` | `0x00001100` | `0x00000815` | 324/27 | 3 | ResultID@0:2, PlayerCount@2:1, InviteUin@3:4[6]{count@2} |
| 394 | `REQUEST_CHANGE_STATUS` | `0x00001140` | `0x000010ED` | 474/12 | 3 | Uin@0:4, Time@4:4, Status@8:4 |
| 395 | `RESPONSE_CHANGE_STATUS` | `0x00001140` | `0x000010EE` | 475/2 | 1 | ResultID@0:2 |
| 396 | `REQUST_LIST_FRIEND_UIN` | `0x00001140` | `0x000010FE` | 485/8 | 2 | Uin@0:4, Time@4:4 |
| 397 | `RESPONSE_LIST_FRIEND_UIN` | `0x00001100` | `0x000010FF` | 486/810 | 5 | ResultID@0:2, Uin@2:4, MaxFriendNum@6:2, CurrentFriendNum@8:2, FriendList@10:4[200]{count@8} |
| 398 | `REQUST_REMOVE_FRIEND` | `0x00001140` | `0x00001102` | 489/12 | 3 | Uin@0:4, Time@4:4, TargetUin@8:4 |
| 399 | `RESPONSE_REMOVE_FRIEND` | `0x00001140` | `0x00001103` | 490/10 | 3 | ResultID@0:2, Uin@2:4, TargetUin@6:4 |
| 400 | `NOTIFY_UPDATE_FRIEND` | `0x00001100` | `0x00001104` | 491/163 | 10 | Uin@0:4, FriendUin@4:4, Point@8:4, ExtItemNum@12:1, ExtItemID@13:4[30]{count@12}, Online@133:1, PlayerNickname@134:1[20], Gender@154:1, Identity@155:4, ExtPoint@159:4 |
| 401 | `REQUST_ADD_FRIEND` | `0x00001100` | `0x00001100` | 487/526 | 5 | Uin@0:4, Time@4:4, TargetUin@8:4, WordLength@12:2, Words@14:1[512]{count@12} |
| 402 | `RESPONSE_ADD_FRIEND` | `0x00001140` | `0x00001101` | 488/165 | 2 | ResultID@0:2, FriendInfo@2:163 |
| 403 | `RESPONSE_ADDFRIEND_RESULT` | `0x00001140` | `0x00001105` | 492/14 | 4 | ResultID@0:2, Uin@2:4, Time@6:4, TargetUin@10:4 |
| 404 | `NOTIFY_ANTI_BOT` | `0x00001100` | `0x00001106` | 493/32006 | 3 | Uin@0:4, BufferLen@4:2, Buffer@6:1[32000]{count@4} |
| 405 | `LOCATION` | `0x00001100` | `0xFFFFFFFF` | 4294967295/26 | 3 | LocationID@0:2, LocationNameLen@2:1, LocationName@3:1[23]{count@2} |
| 406 | `LOCATION_INFO` | `0x00001100` | `0xFFFFFFFF` | 4294967295/1301 | 2 | LocationNum@0:1, location@1:26[50]{count@0} |
| 407 | `REQUEST_HALL_INFO` | `0x00001100` | `0x0000042E` | 325/122 | 7 | Uin@0:4, Time@4:4, Version@8:4, LocationID@12:2, FileHash@14:1[32], FileNum@46:4, cfgFileInfos@50:24[3]{count@46} |
| 408 | `DOWNLOAD_ADDR` | `0x00001140` | `0xFFFFFFFF` | 4294967295/8 | 3 | IPAddr@0:4, Port@4:2, LocationID@6:2 |
| 409 | `RESPONSE_HALL_INFO` | `0x00001100` | `0x00000816` | 326/182934 | 20 | ResultID@0:2, Version@2:4, Build@6:4, Location@10:1301, KingdomStamp@1311:4, ServerCount@1315:1, Servers@1316:12[240]{count@1315}, ChannelCount@4196:1, Channels@4197:17626[10]{count@4196}, AttachInfoLen@180457:2, AttachInfo@180459:1[2048]{count@180457}, DownloadAddrNum@182507:1, DownloadAddr@182508:8[20]{count@182507}, VersionDirLen@182668:1, VersionDir@182669:1[128]{count@182668}, BuildDirLen@182797:1, BuildDir@182798:1[128]{count@182797}, PackageIndex@182926:2, Opintion@182928:2, UpdateChoice@182930:4 |
| 410 | `NOTIFY_P2P_SALE_OVER` | `0x00001100` | `0x0000117A` | 461/210 | 4 | Uin@0:4, DstUin@4:4, ReasonLen@8:2, Reason@10:1[200]{count@8} |
| 411 | `REQUEST_DOTASK_NEW` | `0x00001140` | `0x00000BD7` | 383/12 | 4 | Uin@0:4, Time@4:4, TaskID@8:2, OprID@10:2 |
| 412 | `REQUEST_TASKINFO` | `0x00001140` | `0x00000BD9` | 385/8 | 2 | Uin@0:4, Time@4:4 |
| 413 | `RESPONSE_DOTASK_NEW` | `0x00001140` | `0x00000BD8` | 384/16 | 6 | Uin@0:4, Time@4:4, ResultID@8:2, TaskID@10:2, OptID@12:2, Status@14:2 |
| 414 | `TaskInfo` | `0x00001100` | `0xFFFFFFFF` | 4294967295/26 | 6 | TaskID@0:2, TaskGrades@2:2, TaskTime@4:4, GameFinished@8:4, Status@12:2, TaskLevel@14:2[6]{count@2} |
| 415 | `RESPONSE_TASKINFO` | `0x00001100` | `0x00000BDA` | 386/166 | 4 | Uin@0:4, Time@4:4, Tasks@8:2, Taskqueue@10:26[6]{count@8} |
| 416 | `REQUEST_BREAK_EGG` | `0x00001140` | `0x00000BDB` | 387/16 | 4 | Uin@0:4, Time@4:4, HammerID@8:4, EggID@12:4 |
| 417 | `RESPONSE_BREAK_EGG` | `0x00001100` | `0x00000BDC` | 388/263 | 4 | ResultID@0:2, ShowItemID@2:4, AttachInfoLen@6:1, AttachInfo@7:1[256]{count@6} |
| 418 | `REQUEST_BACKGROUND_LIST` | `0x00001140` | `0x00000BE0` | 389/12 | 3 | Uin@0:4, Time@4:4, Reserve@8:4 |
| 419 | `RESPONSE_BACKGROUND_LIST` | `0x00001100` | `0x00000BE1` | 390/212 | 4 | Uin@0:4, Time@4:4, ListNum@8:4, ListID@12:4[50]{count@8} |
| 420 | `REQUEST_USE_BACKGROUND` | `0x00001140` | `0x00000BE2` | 391/12 | 3 | Uin@0:4, Time@4:4, ItemID@8:4 |
| 421 | `RESPONSE_USE_BACKGROUND` | `0x00001140` | `0x00000BE3` | 392/10 | 3 | Uin@0:4, Time@4:4, ResultID@8:2 |
| 422 | `NOTIFY_USE_BACKGROUND` | `0x00001140` | `0x00000BE4` | 393/12 | 3 | Uin@0:4, Time@4:4, ItemID@8:4 |
| 423 | `QUESTIONITEM` | `0x00001100` | `0x0000179B` | 583/258 | 3 | QuestionId@0:1, QuestionDesLen@1:1, QuestionDes@2:1[256]{count@1} |
| 424 | `SPPQUESTION` | `0x00001140` | `0x0000179C` | 584/2066 | 3 | QuestionCount@0:1, Question@1:258[8], QuestionNum@2065:1 |
| 425 | `SPPMOBILEPHONE` | `0x00001100` | `0x0000179D` | 585/771 | 6 | MobileLen@0:1, Mobile@1:1[256]{count@0}, AccessNoLen@257:1, AccessNo@258:1[256]{count@257}, SmsLen@514:1, Sms@515:1[256]{count@514} |
| 426 | `SPPCARD` | `0x00001100` | `0x0000179E` | 586/517 | 7 | CardNoLen@0:1, CardNo@1:1[256]{count@0}, CoorNum@257:1, CoorSize@258:1, CardCoorLen@259:1, CardCoor@260:1[256]{count@259}, Expired@516:1 |
| 427 | `SPPIVR` | `0x00001100` | `0x0000179F` | 587/516 | 5 | PhoneLen@0:1, Phone@1:1[256]{count@0}, DestNoLen@257:1, DestNo@258:1[256]{count@257}, Time@514:2 |
| 428 | `SPPITEM` | `0x00001140` | `0x000017A0` | 588/4100 | 4 | MbItemId@0:1, Use@1:1, ContentLen@2:2, Content@4:1[4096] |
| 429 | `NOTIFY_REQUESTSPPKEY` | `0x00001100` | `0x000017A1` | 589/65606 | 4 | Result@0:4, CommMbItem@4:1, MbItemNum@5:1, Item@6:4100[16]{count@5} |
| 430 | `ANSWERITEM` | `0x00001100` | `0x000017A6` | 594/258 | 3 | QuestionId@0:1, QuestionAnsLen@1:1, QuestionAns@2:1[256]{count@1} |
| 431 | `RESQUESTION` | `0x00001140` | `0x000017A2` | 590/2065 | 2 | QuestionNum@0:1, Answer@1:258[8] |
| 432 | `RESMOBILEPHONE` | `0x00001100` | `0x000017A3` | 591/257 | 2 | MobileCodeLen@0:1, MobileCode@1:1[256]{count@0} |
| 433 | `RESCARD` | `0x00001100` | `0x000017A4` | 592/516 | 6 | CoorNum@0:1, CoorSize@1:1, CardCoorLen@2:1, CardCoor@3:1[256]{count@2}, CardPwdLen@259:1, CardPwd@260:1[256]{count@259} |
| 434 | `RESTOKEN` | `0x00001100` | `0x000017A5` | 593/257 | 2 | TokenCodeLen@0:1, TokenCode@1:1[256]{count@0} |
| 435 | `RESSPPITEM` | `0x00001100` | `0x000017A7` | 595/4099 | 3 | MbItemId@0:1, VerifyContentLen@1:2, ItemVerifyContent@3:1[4096]{count@1} |
| 436 | `REQUEST_RESPONSESPPKEY` | `0x00001100` | `0x000017A8` | 596/8203 | 3 | Uin@0:4, MbItemNum@4:1, ItemAns@5:4099[2]{count@4} |
| 437 | `RESPONSESPPKEY` | `0x00001100` | `0x000017A9` | 597/262 | 3 | Result@0:4, MsgLen@4:2, Msg@6:1[256]{count@4} |
