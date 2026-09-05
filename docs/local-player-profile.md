# 本地人物档案配置

本地人物的初始属性统一配置在 `configs/server-directory-local-ui.json` 的 `player_profile` 节点。首次用某个本地 UIN 登录后，服务端把完整类型化档案写入 `runtime/data/qqtang.sqlite`；此后 SQLite 是该 UIN 的运行时数据源，JSON 继续作为新账号种子。服务端不会修改原始游戏数据包。

人物档案由 `PlayerProfile` 定义，战绩/等级由 `GameInfo` 定义，背包项由 `ItemInfo` 定义。`PlayerProfile.ToLocalLoginConfig` 会显式转换为 `LoginResponse`，再由类型自身编码 `0x07D5 / RESPONSE_LOGIN`；业务代码不再手工填写字段偏移。完整约定见 [protocol-modeling.md](protocol-modeling.md)。

装扮分为两个持久化维度：账号公共所有权位于`player_inventory(uin,item_id)`，角色穿戴位于`player_loadouts(uin,role_id,slot)`。GM只授予公共库存；客户端商城按角色保存套装。同一库存物品只有一个当前装备角色，切换角色会转移该物品；不同角色的独立套装需要引用不同的已拥有物品。

## 当前本地纪念档案

- `game_info.wins/losses/draws`：虚构战绩 `5200 / 520 / 52`。
- `game_info.points`：`1621150000`，接近本版本上限但避开最后积分边界。
- `game_info.degree`：`179`，比客户端最高等级低一级，避免满级边界行为。
- `game_info.money`：`99999999`。
- `tutorial_completed`：`true`。若胜、负、平三项全部为零，服务端会在登录响应里写入一个中性的平局记录作为已完成教程标记；存在自定义战绩时保留自定义值。
- `inventory`：永久包含 `{ "id": 99, "quantity": 1 }`，即单人探险卡。客户端自带 `QQTMsgData.bin` 已将 18 字节道具结构命名为 `ITEM_INFO`，尾字段是 `AvailPeriod`；旧客户端会在数量或有效期为零时删除物品。人物 JSON 无需暴露这个协议细节，服务端会仅对 ID 99 自动编码本地永久有效期。

SQLite 表 `local_players` 以 UIN 为主键，`profile_json` 保存同一个 `PlayerProfile` 的 JSON 表达，另有创建/更新时间。这样 Go 协议层始终使用 `PlayerProfile`、`GameInfo` 和 `ItemInfo`，数据库不会产生第二套魔法偏移。地图选择不属于人物档案，它只保存在当前房间连接内存中，离开房间或断线即失效。

如果需要重新应用修改后的种子 JSON，应先在客户端和服务端均停止后，备份并删除 `runtime/data/qqtang.sqlite`；下次登录会重新创建该 UIN。不要在服务运行时直接改库。

## 字段边界

服务端拒绝以下配置，避免旧客户端越界：

- `player_id`、`section_id` 或 `minimum_room_id` 为零；
- `game_info.degree` 大于 `180`；
- `game_info.points` 大于 `1631150000`；
- 背包物品 ID、数量或有效期为零，或物品数超过 `500`；人物 JSON 中的 ID 99 会自动补本地永久有效期；
- `reason` 超过 `200` 字节。

昵称和 UIN 当前不由该节点伪造：昵称来自客户端登录请求，UIN 必须与本地加密信封一致。等多客户端账号分配协议完成后，再将二者纳入持久档案。

## 等级上限证据

随客户端发布的 `res/uiRes/levelCFG.pyc` 是 Python 2.3 字节码，共 `6452` 字节，SHA-256 为 `9170CD4C0E9B686C14E131290BE67A2A906974D6828EF69EF669252016022656`。其 `LevelCfg.points` 含 `181` 个阈值，最后一个是 `1631150000`；`__point2level` 对超过表尾的积分返回 `len(points)-1`，因此最高内部等级为 `180`。`getInfo` 以每组六级换算为第 30 大段第 6 小段。

复核命令：

```powershell
$env:PYTHONPATH=(Resolve-Path 'runtime\pytools').Path
python -m xdis.disasm runtime\client-patched\res\uiRes\levelCFG.pyc
```
