# 角色装扮与背包持久化

## 已确认的客户端流程

客户端把装扮分成三个层次：

1. `itemList` 是账号物品目录，记录物品 ID、分类、客户端表现索引和名称。
2. `InitPlayerActiveItems()` 以当前 `RoleID` 和分类调用 `GetRoleItem(RoleID, type)`，再用 `shopPlayer_wear(type, index, effect, color)`恢复角色外观。
3. 商店中的试穿只改变本地预览；用户保存时，`sendActiveItems()` 才调用 `ActiveItemStatus(uin, currentRoleID, count, itemIDs)`。

因此服务端采用以下边界：

- `player_inventory`：账号拥有的物品及数量。
- `player_loadouts`：`(uin, role_id, slot) -> item_id`，保存每个角色自己的套装；同时以 `(uin, item_id)` 唯一约束保证一件库存物品只有一个当前装备角色。
- `player_seed_history`：只记录已经应用的初始装扮版本，防止每次启动覆盖玩家后来在商店保存的结果。
- `ITEM_INFO.status/role_id`：只作为旧客户端登录协议的投影，不再作为数据库装备关系的唯一真值。

同一账号物品不复制库存，也不能同时挂在多个角色套装里。商城把它装备到另一角色时是一次转移：旧角色引用先删除，再写入新角色和槽位。登录/商城档案与房间表现虽然使用不同旧协议结构，但都投影同一个当前 `ItemRoleID`。客户端物品定义允许某些通用物品忽略角色号；这种情况以 `ItemRoleID=0` 表示通用或未绑定，不等于数据库保存多个角色引用。角色适配矩阵尚未完整恢复，保存时仍以客户端 `GetAllowWear` 的结果为准。

## 装备槽

物理槽来自 `res/uiRes/uiConst.py` 的 `equipMap`：

- `cap/fhadorn -> head_front`
- `bhadorn -> head_back`
- `thadorn -> head_top`
- `hair -> hair`
- `mask/eye -> face`
- `mouth -> mouth`
- `ear -> ear`
- `cloth -> cloth`
- `cladorn -> cloth_adornment`
- `fpack -> front_pack`
- `npack -> back_pack`
- `leg -> leg`
- `foot -> foot`

另有不直接改变角色身体图层的逻辑槽：`bubble`、`phantom`、`footprint`、`background`、`frame`、`entrance`、`namecard`、`namecard_bound`。

## 当前初始套装

`configs/starter-loadouts.json` 是当前唯一初始套装清单。它给账号 `1000001`
的角色 7 授予并装备 16 个互斥槽示例，覆盖入场、帽饰、前后背饰、背景、
名片/边框、糖泡、幻影、脚印、服装等分类。version 848 的官方 ItemZips 已在
构建阶段恢复到客户端，物品 ID、分类、resource ID 与目标路径由
`data/qqt_item_resources.json` 闭合，不再用同号 NPC/地图图标猜测资源。

seed key 只应用一次，不会在每次启动时覆盖玩家后来于商城保存的套装。现有
SQLite 升级到 schema 7 时只规范化同一物品的多角色冲突，不会重放 seed；用户
背包和其余槽位保持不变。

## 保存语义

`0x0085 / REQUEST_ITEM_STATUS_CHANGE` 同时被房间快捷栏和商城保存使用：

- 非装扮物品继续更新 `player_inventory.item_status/item_role_id`。
- 能在客户端目录中归类到装扮槽的物品，状态 1 表示把它写入请求角色的槽；其他状态表示从该角色槽卸下。
- 同槽换装通过 SQLite 主键 `(uin, role_id, slot)` 原子替换。
- 同一物品换角色通过 SQLite schema 7 的 `(uin,item_id)` 唯一索引原子转移；旧版多角色脏数据迁移时保留最后写入行。
- 试穿没有该请求，不写数据库。

## 商城收藏柜

商城物品详情里的“收藏”与“回收柜/恢复”也复用 `0x0085`，但它与角色穿戴是正交状态：

- `ChangeItemStatus(uin, itemID, 2)`：放入收藏柜；
- `ChangeItemStatus(uin, itemID, 0)`：从收藏柜恢复；
- `ActiveItemStatus(...)`：保存角色穿戴，活动项投影为状态 1。

`uiShop.py` 的 `Recycler` 通过 `GetDisableItemCnt/GetDisableItemInfo`读取状态 2 的物品，证明收藏柜不是第二张库存表。服务端把状态 2 保存在`player_inventory.item_status`，同时以`player_loadouts`保存独立穿戴关系；收藏已穿戴装扮时，状态更新和删除其 loadout 在同一 SQLite 事务完成。恢复只把状态改回 0，不会偷偷重新穿戴。装备投影只改写状态 0/1，不再抹掉状态 2。

商城购买已经接入 `0x0259 / REQUEST_BUY(0x040D) / RESPONSE_BUY(0x07F5)`。客户端 `uiShop.py` 明确传入的支付类型为：Q币 1、糖币 3、VNet 4、酷比宝石 6、Q点 7；两个“余额不足时用另一种余额补足”的复选框只进入 `AgreeMixedPayment`。本地服只结算糖币 3；外部支付路径根据类型和该标志返回明确的余额不足提示，绝不改变数据库。糖币路径不受外部兜底标志影响。可见商品和服务端结算共用同一份按原始资源分类均衡抽取的 1000 项 `Commodity.ini` 目录，`CommodityID` 只用于定位商品，最终入库始终使用 canonical `ItemID`。

商城的两个原生传输类型不能混成一种消息类型，但使用同一个 QQTDir 商城目的端点：Type3 连接发送 `0x035E` 商品目录，Type2 对象承载配置协商和 `0x0259` 购买。`EnterShop` 只接收一个 ServerID，QQTShop 将解析出的同一组 IP/端口字段传给两类对象。Go 的 `18001` 监听器按消息类型分派，每条 TCP 连接独立标记为辅助连接，其关闭不能注销 `18000` 大厅主会话。配置和购买响应完整写出后服务端释放对应的一次性 Type2 连接，防止旧同端点对象截获下一次连接完成事件。诊断阶段把购买改写成 Type3、拆到 `18002` 或增加连接延迟的方案均已删除或排除。

目录新旧由进入商城时的独立 `REQUEST_SHOPLIST` 流程协商。原生 `BuyCommodity` 绑定只显式填写商品、交易方式、支付方式和混合支付标志，购买结构里的 `CommodityVersion` 由旧核心兼容层补写，实机证明其值不能作为可靠的一致性令牌。因此结算不再用该字段拒绝请求，而是以当前服务端已发布目录中的 `CommodityID` 为权威；请求值与服务端版本仍写入结构化日志供协议诊断。这样不会放宽商品或价格校验，未知/下架商品仍然拒绝，价格仍完全取服务端目录。

糖币扣除与 `player_inventory` 入库位于同一个 SQLite 事务。可堆叠道具增加数量；永久装扮已经拥有时拒绝重复购买，避免无意义数量累积。`0x07F5 RESPONSE_BUY` 负责交易结果、余额和清除商城忙锁，但原版商城右侧背包使用大厅人物档案缓存；成功响应后还必须经 `18000` 主大厅连接发送 `0x081D NOTIFY_PLAYER_ITEMADD` 的完整 `ITEM_INFO`，才会在线出现新物品。该通知不能发在可随时关闭的商城辅助 socket。实机已确认新装扮无重登即时出现。TCP 分派调用商城处理器时已经持有当前连接的 `session.mu`，成功路径不得再次获取同一非重入锁；定向测试在这一真实锁层次下执行购买，防止再次出现“余额不足能响应、糖币成功路径无反应”。随后客户端通过 `0x0085` 保存当前角色的穿戴槽，公共库存与角色级 loadout 的边界不变。

大厅玩家行使用 `PLAYER_INFO_OLD.ExtItemIDs` 表现名片；房间角色使用
`PLAYER_INFO_IN_ROOM_OLD.Items` 经 `useCommItem/room_playerWear` 表现身体与
房间装饰。两个结构都由同一份 `player_loadouts` 生成，但不能混用字段布局。
