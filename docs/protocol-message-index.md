# 协议消息索引

| 状态 | 运输命令 | 方向 | 传输 | schema/长度 | 证据与说明 |
|---|---:|---|---|---|---|
| Local compatibility | `QQTAUTH1` | 登录 helper ↔ 游戏服 | 与游戏服相同 TCP 地址，正式 QQT 分帧前 | Hello/Challenge/Proof/Result；最大 1024 字节 | 非腾讯 wire schema，不写普通 capture。SCRAM 风格 proof 接受后只授予同主机/UIN 一次、30 秒有效的 `0x0064` 许可；错误密码立即拒绝。见 EVID-143。 |
| Confirmed | `0x0133` | 客户端 ↔ 目录服务 | TCP `127.0.0.1:18080` | 响应 `0x0816 / RESPONSE_HALL_INFO` | 请求与响应保留同一运输命令；客户端真实显示 `Local / Local Section` |
| Confirmed | `0x0064` | 客户端 → 游戏服 | TCP `127.0.0.1:18000` | 请求 `0x03ED`；首包 210 字节 | ST/UIN/QQ-TEA 解密和两次自然请求均已验证 |
| Confirmed | `0x0064` | 游戏服 → 客户端 | TCP `127.0.0.1:18000` | 响应 `0x07D5 / RESPONSE_LOGIN`；202 字节 | Go 动态构造；客户端接受并进入教程和大厅 |
| Confirmed | `0x0065` | 客户端 ↔ 游戏服 | TCP | `0x03EE / REQUEST_LOGOUT`：9+5×count 字节；`0x07D6 / RESPONSE_LOGOUT` | 现场样本为 UIN+Time+零统计项，Route 2/Section 1；发生在旧游戏服连接结束并立即重连大厅之前。服务端必须先返回 ResultID=0，并清理旧连接的房间与大厅在线状态。 |
| Confirmed | `0x0066` | 客户端 ↔ 游戏服 | TCP | `0x03EF / REQUEST_ROOM` 15 字节；`0x07D7 / RESPONSE_ROOM` | 请求末两字节是 GameMode/GameType；wire GameMode=客户端菜单值+1（0 所有房间、1 全部地图、2 普通等），GameType=场类型 0/1/2/3/4。响应回显筛选与分页游标。 |
| Confirmed | `0x0069` | 客户端 ↔ 游戏服 | TCP | `0x03F9 / REQUEST_LEAVE_ROOM` 8 字节；`0x07E1 / RESPONSE_LEAVE_ROOM` | 准备房实际点击离开时唯一出现，主体为 UIN+Time；服务端移除成员、迁移房主或销毁空房。旧“玩家信息刷新”命名已撤回。 |
| Confirmed | `0x006B` | 客户端 ↔ 游戏服 | TCP | 请求 `0x03F2 / REQUEST_CREATE_ROOM` 固定 50 字节；响应 `0x07DA / RESPONSE_CREATE_ROOM` | 完整请求字段为 UIN、Time、RoomName[20]、Flag、Password[16]、GameType、ContinueID；响应含 Result 与 RoomID。默认随机地图不会另外携带一个可继承的固定 MapID。 |
| Confirmed | `0x0070` | 客户端 ↔ 游戏服 | TCP | 请求 `0x0400 / REQUEST_CHANGE_TERM` 10 字节；响应 `0x07E8` | TermID(uint16) 是房间八色 TeamID 1..8；服务端更新实时成员并返回 ResultID=0。绿色 TeamID 4 已实测。 |
| Confirmed | `0x0071` | 客户端 → 游戏服 | TCP | 请求 `0x0401 / REQUEST_CHANGE_ROLE`；payload 固定 10 字节 | UIN(4)+ClientTime(4)+RoleID(uint16)；多轮真实样本和 `UserSelRoleInRoom` 静态路径一致。RoleID 23 是房间内保持问号的随机占位符，只在 `GAME_BEGIN` 对局快照中按模式解析，不写账号存档；13–16 是紫钻角色且永不随机。 |
| Confirmed | `0x007A` | 游戏服 → 客户端 | TCP | `0x0409 / NOTIFY_CHANGE_TERM` 7 字节 | PlayerUin+PlayerID+NewTermID；房间 Route 3/RoomID。`TermAndSeat=0x41` 和关卡绿色实测证明 TeamID 4 已进入 GAME_BEGIN。 |
| Confirmed | `0x0078` | 客户端 ↔ 游戏服 | TCP | 请求 payload 固定 15 字节 | 房主明确修改当前房间的 MapID/GameType；随机建房时可以完全不发送该命令，因此固定选择必须归属房间、不能缓存到连接或人物持久化。 |
| Confirmed | `0x007F` | 客户端 ↔ 游戏服 | TCP | `0x0417 / REQUEST_SET_SEAT_STATUS` 10 字节；`0x07FF` 响应；`0x0418` 通知 | 字段为 UIN、Time、SeatID、NewStatus；探险建房实测以状态 2 锁座位 5..8。房间列表容量由未锁座位数产生。 |
| Confirmed layout | `0x007D/0x007E` | 客户端 ↔ 游戏服 | TCP | `0x0415` 请求 12 字节；`0x07FD` 响应 2 字节；`0x0416` 通知 10 字节 | 房间/大厅账号物品使用：UIN+Time+ItemID；通知中间 uint16 原名 `Dlg`，现保留为 `DialogCode`。与战场临时道具 `0x0FAF` 完全不同，未恢复配方持久化前不消费。 |
| Observed | `0x006A` | 客户端 → 游戏服 | TCP | 独立帧 90 字节 | 业务含义 Unknown |
| Observed | `0x006E` | 客户端 → 游戏服 | TCP | 独立帧 98 字节 | 业务含义 Unknown |
| Confirmed | `0x0083` | 客户端 ↔ 游戏服 | TCP | `0x03FB / REQUEST_PLAY`：UIN+Time+uint16 长度+GameData | 客户端战斗请求及同命令关联响应；已确认 `0x0FA7` 死亡、`0x0FAE` 物品、`0x1178/79` 换关请求/响应。 |
| Confirmed | `0x0084` | 游戏服 → 客户端 | TCP | `0x041B / NOTIFY_GAME_EVENT`：RoomID+uint32 GameDataSeq+uint32 长度+GameData | 服务器主动战斗事件；GameData 以小端 DWORD schema 开头。用于 `0x0FA7`、`0x0FAE`、`0x0FBB`、`0x1177`，路由关联序号为零并使用独立事件序号。 |
| Confirmed | `0x0089` | 游戏服 → 客户端 | TCP | `0x041C / GAME_BEGIN_DATA` → `0x0FA1 / NOTIFY_GAME_BEGIN` | `ID_SMC_NOTIFYGAMESTART`；携带完整开局对象，不是空状态包。 |
| Observed | `0x0088` | 客户端 → 游戏服 | TCP | 独立帧 90 字节 | 业务含义 Unknown |
| Observed | `0x0097` | 客户端 → 游戏服 | TCP | 独立帧 98 字节 | 业务含义 Unknown |
| Confirmed | `0x009A` | 游戏服 → 客户端 | TCP | `0x0427 / NOTIFY_POINTS_REFRESH`；最小 payload 61 字节 | `ID_SMC_NOTIFYPOINTSREFRESH`；含普通/探险胜负计数、Point、ExtPoint、Money、Honor 和最多 15 个 PATTERN_POINT。历史所谓“客户端 994 字节独立帧”没有通过当前严格逐帧/方向校验，不再作为该命令的语义证据。 |
| Confirmed layout | `0x008B/0x0094` | 客户端 ↔ 游戏服 / 游戏服 → 客户端 | TCP | `0x0421/0x0809` 查询余额；`0x041F` 钱币通知 11 字节 | 查询命令与主动通知是两个 transport command。通知为 ResultID+MoneyType+绝对 Money+UIN；MoneyType 业务枚举未确认。Boss 钱币不能伪装成 GAME_OVER.Point。 |
| Confirmed | `0x00D9` | 游戏服 → 客户端 | TCP | `0x081D / NOTIFY_PLAYER_ITEMADD`；18 字节头 + `ITEM_INFO[ItemNum]` | 动态路径为 NetCenter 解码 `0x081D` 后调用 QQTSection 回调槽 143；旧九字节 `0x03FC` 负载实测解码为空对象。正式完整绝对 ITEM_INFO 使用人物档案 Route 2/Section 1；药水扣减和结算拾取批量刷新均已实测。 |
| Observed | `0x00A7` | 客户端 → 游戏服 | TCP | 已见 90–218 字节独立帧 | 业务含义 Unknown；长度可变 |
| Observed | `0x0107` | 客户端 → 游戏服 | TCP | 独立帧 82 字节 | 业务含义 Unknown |
| Observed | Unknown | 客户端 → 游戏服 | UDP `127.0.0.1:18000` | 固定 24 字节 | 登录后连续发送；用途 Unknown |

主要原始证据：`runtime/captures/directory-local-ui/session-20260804-174306.055677300/capture.jsonl` 和 `runtime/logs/server-game-login-command64-v295.stdout.log`。

当前运行中的 v52 以 TCP `Read` 为 capture 粒度，少量记录可能包含多个粘连帧；上表长度只采用能够独立通过包长校验并成功解密的帧。源码的逐帧拆分测试已通过，下一轮部署后再登记严格出现次数。

合成探针向量 `5151540001` 只用于早期 capture 管线测试，不属于 QQ堂协议索引。

## 战斗 GameData 连续消息族

`QQTMsgData.bin` 的物理相邻记录已按组批量枚举，不再每次只登记一个字段：

- 死亡/NPC：`0x0FA7`，以及 `0x1169..0x1176` 的 NPC 技能、对话、独立掉落、宝藏、游戏时间、预备道具和地图元素事件；物理相邻处另有 `0x1194` 表情。
- 玩家交互：`0x0FA8/0x0FA9` 击杀请求/通知，`0x0FAA/0x0FAB` 救援请求/通知。
- 拾取/分发：`0x0FAC/0x0FAD` 拾取请求/通知，嵌套 `ITEM_FROM_SERVER`，`0x0FAE` 初始/延时物品分发，下一项为 `0x0FAF REQUEST_USE_ITEM`。
- 移动聚合：`0x10EB QQT_PACKAGE_TO_SERVER` 为 PlayerID+Time+最多 255 个动态 `QQT_MSG_DATA`；`0x10E7 QQT_PACKAGE_TO_PLAYER` 为 GameID/PlayerID/FirstIndex、最多 2 个绝对移动、16 个有符号增量移动和 8 个普通消息。`0x0FA2 PLAYER_MOVE` 为 PlayerID/Count/SeqReply，再发送全部 PlayerIDs 与全部 20 字节 MoveSeqs。原版 QQTEncoder 五个向量已逐字节验证。外层 socket/帧仍未知；TenQQt 的 P2P/UDP 文本已经证明属于下载器，不能据此推断对局传输。恢复项目选择中心服聚合，但不能把上行原包原样广播。
- 携带型目标：`0x0FB6/0x0FB7` 拿取、`0x0FB8/0x0FB9` 放下，均为 `PlayerID+Time+PosX+PosY+BunID+BunTeamID` 的 12 字节结构。字段名来自 `QQTMsgData.bin`，不能再改称 `ActionKind/ObjectiveID`。规则3中 `BunID=0` 是基地包子、`BunID=1` 是场景散落包子；规则6中拾取固定`BunID=2`、放置固定`BunID=0`，`BunTeamID=12..16`为材料类型，同队雕像累计四次完成。
- 原生耐久：`0x10F4 PLAYER_BE_HARMED` 为 `PlayerID+Time+PosX+PosY+IsAvatar+LossHP` 的 13 字节结构。规则7机械世界和规则8箱子由客户端先本地应用，每次非变身伤害消耗四层耐久中的一层，`LossHP` 不作为标量；服务端只发其他成员。客户端每次非变身伤害都会提前上报 `0x0FA7`，但本地只在耐久归零时执行死亡，因此服务端 ACK/过滤前三次、仅广播并结算第四次。TCP 与快速包使用同一去重状态。
- 推箱固定装置：`0x115E/0x1163` 是玩家请求并定向唯一仲裁者；仲裁者产生 `0x115F/0x1160/0x1167/0x1168`，上传前均已在本地执行，所以可靠通知只发其他成员。`0x1167` 为 `PlayerID+Time+Fire`，`Fire` 是四角固定发射器的非零低四位掩码，不是 SceneID/技能/AIType。`0x1161/0x1162/0x1166` 是物理观察，同样只中继给其他成员；`0x1164/0x1165` 保持客户端内部消息。约60秒的 `0x1389` AIType 2 NPC是独立链。
- 原生局内道具：`0x10E2/0x10E3` 吃泡和 `0x10F5/0x10F6` 取消持续道具都是请求→通知对，不能原 schema 回显；`0x10F7` 是仲裁者生成的完整触发结果，含最多 10 个受影响玩家。

完整 schema、主体大小、原始字段顺序、静态地址与证据等级见 `evidence/combat-event-message-family-v1.md`。Go 已强类型化当前同步闭环需要的 `0x0FA8..0x0FAD` 和 `0x116B`；其余记录先保留表中原名，不用臆测替换未知类型。

## 完整 QQTMsgData 元数据

`cmd/qqt-msgtable` 已按完整元数据头签名把 `runtime/client-patched/QQTMsgData.bin` 的 438 条消息/嵌套结构一次性导出，不再依赖类别白名单。索引 `0..437` 连续且无重复；类别为 `0x1100/0x1140/0x1500/0x1540`，140 个非空嵌套类型引用全部可解析。`evidence/qqt-message-table.json` 保存消息头和字段的全部原始 DWORD，`evidence/qqt-message-table.csv` 每字段一行，`docs/protocol-message-table.md` 是完整紧凑索引。字段名、字节偏移、容量、元素宽度和动态计数字段引用来自二进制元数据；未解明的类型代码保留原值，不擅自赋予 C++ 类型语义。

同一生成器现也确定性导出 154 条运输名称记录（151 个唯一命令）、207 个请求/响应/通知/确认消息族，以及 118 条仅按规范化原名建立的命令/schema 家族候选。入口分别是 `docs/protocol-transport-commands.md`、`docs/protocol-message-families.md` 和 `docs/protocol-command-schema-candidates.md`；机器可读版本位于 `evidence/qqt-*.json/.csv`。`scripts/find-protocol.ps1` 可在 Windows PowerShell 5.1 下按 command、schema 或名称直接查询并展开字段。候选等级固定为 `normalized_name_only`，CMS/SMC 前缀和名称匹配都不能代替实机方向与消费函数证据；完整限制和房间协议预解析见 `evidence/protocol-preanalysis-v1.md`。

## 批量代码路由目录

`scripts/export-message-routes.ps1` 将 366 个具体 schema 在 Client 与 QQTSection 中一次性扫描。长期 CSV 位于 `evidence/client-message-routes*.csv` 和 `evidence/qqtsection-message-routes*.csv`：Client 得到 532 个用途/106 个 schema/304 个函数，QQTSection 得到 134 个用途/87 个 schema/109 个函数。扫描只收立即数而排除结构偏移；结果仍是静态候选，数值碰撞必须结合模块上下文。

动态补全由 `scripts/capture-message-routes.ps1` 启动：它挂接 Client 公共消息分发器，一轮最多记录 8192 条真实的 `schema -> handler vtable -> vtable+4 处理方法 -> vtable+8 接受谓词` 映射。公共调用参数已由首轮故障证明可能是小整数 token/offset，现只保存数值而不解引用。实际死亡轮次已捕获 `0x0FA7 -> 0x00606606` 与 `0x0FBB -> 0x00602BAB`；后者继续跳到受保护的 `0x00881E00`。其独立入口快照进一步确认实际对象含 `Point=500, FieldCount=0`：奖励值已到 handler，四组扩展仍未发送。完整方法、限制与结算消息验证样本见 `evidence/message-route-catalog-v1.md`。

受控字段差分已进一步映射 `0x0FBB`：前三组 `FieldValue1/FieldValue2` 分别是杀敌、救援、奖励的计数/得分；第 4 组未在失败界面显示，业务名仍为 Unknown。结算后的 `ExtPoint` 增量等于全部四个 `FieldValue2` 之和，而不是 `Point`。编码器因此保留最多四组原始字段，只为前三槽提供具名视图，并用全槽得分总和计算探险成长。
