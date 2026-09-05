# Go 服务端分层

服务端按“协议适配器 → 应用用例 → 领域状态 → 持久化”分层。原版客户端仍使用其固有线协议，但账号、商城、背包、宠物、房间和对局规则不再依赖 TCP 连接或加密包才能运行和验证。

## 依赖方向

```text
cmd/qqt-server
  -> internal/server/probe             TCP/UDP、协议选择、发送顺序、旧客户端适配
       -> internal/server/application  面向用例的 PlayerService / World
            -> internal/game/*         lobby / room / match / equipment / catalog
            -> internal/server/persistence  SQLite 仓储
       -> internal/protocol/*          纯线协议类型和编解码
       -> internal/clientdata/*        可由原客户端验证的静态语义
```

下层不能反向依赖 `probe`。例如糖币购买由 `PlayerService.PurchaseCommodity` 完成；`probe/shop.go` 只把 Type2 的 `0x0259` 请求转换为支付方法，再把用例结果编码为旧客户端响应。`application.World` 组合 `game/lobby` 与 `game/room`，因此可以在没有 `Client.exe`、socket 或 QQTEA 数据包的情况下运行双玩家生命周期。活动适配器与无客户端自检现已使用同一个 World 模型；`probe.Server` 不再持有第二份房间 map、大厅 Hub、RoomID 或 GameID 分配器。

## 协议层

- `internal/protocol/game/commands.go`：所有已确认的顶层 transport command、所属协议族、方向和唯一注册表。
- `internal/protocol/game/enums.go`：跨消息使用的枚举族和位集合，包括身份、房间列表/座位状态、规则/结算模式、结算结果和商城支付。
- 具体消息文件：只保留该消息的 schema ID、固定/变长布局、大小上限与编解码函数。
- `commands_test.go`：拒绝重复 command ID、重复名称、空 family/direction；请求/响应确实共用的 `StartGameResponseCommand` 以显式别名保留。

协议常量不能重新散落到处理器或消息文件。若确认新命令，先登记 `commands.go`；若只是某个 payload 内部的事件 ID、schema 或数组上限，则继续放在相应 codec 文件，不能混入 transport command 注册表。

## 应用层

`PlayerService` 当前提供：

- 人物加载、创建、保存与角色穿戴投影；
- 糖币事务购买以及外部币种的业务拒绝；
- 背包状态/穿戴变更、快捷栏消耗；
- 探险经验与掉落结算、普通竞技经验/胜负结算，以及可选的独立钱币奖励（统一封顶）；
- 宠物创建、列表与删除。

`World` 当前提供：

- 大厅进入/离开与快照；
- 创建、加入、离开、销毁房间；
- 房主迁移、座位、角色、队伍、准备和房间属性；
- 开局、结算以及正常结算后保留房间成员。
- 只读 `WorldSnapshot`，一次导出大厅、房间完整快照和 UIN/PlayerID/RoomID 成员关系，供日志、测试与后续 GM 诊断使用。

`PlayerService` 和 `World` 均已被活动协议适配器使用。创建/加入/离开、房间属性、角色/队伍/准备/锁席、开局/结算、房间列表以及 QQTPPP 同房鉴权都从 World 读取或变更。`connectionSession` 只保留当前 TCP 对象需要的 UIN、Profile、RoomID/GameID 投影和发送状态；这些字段不是第二个房间目录，也不能反向覆盖 World。

这些 API 不接受完整协议包，也不返回加密字节。为兼容已经恢复的大量旧消息，人物用例当前仍使用
`protocol/game.PlayerProfile` 作为边界投影；它只是一份旧客户端 DTO，不是大厅、房间或对局的第二份
权威状态。后续新增领域状态不得继续把原始 payload、sequence、route 或加密帧传入应用层。

`internal/clientdata/sceneelement` 是一项刻意很小的共享静态目录：它集中原客户端场景工厂 ID、已证实
奖励语义和可构造范围。竞技对局不再反向依赖协议包来解释奖励，协议层只保留兼容别名。账号库存 ID
与场景对象 ID 因此继续保持两个命名空间，不能因数值相同而互换。

## 探针适配层

通用 `handleTCPMessage` 现在只做认证帧旁路、抓包、快速包识别、调用分发管线和交付结果。普通 TCP
请求由 `tcp_dispatch_pipeline.go` 按明确优先级经过目录、商城、登录、聊天、背包状态、房间、QQTPPP、
余额、大厅、开局和对局事件阶段；每个阶段只能调用所属协议族适配器。原先在 socket 循环中维护的
十余组 response/follow-up/settlement 临时变量已归并为不持有 socket 的 `tcpDispatchOutcome`，发送仍由
`tcp_sequence.go` 负责。协议选择和有序交付由此可以分别测试。

其余处理器按职责拆为：

- `shop.go`：同一 `18001` 商城监听器上的 Type3 商品目录与 Type2 配置、购买；客户端使用独立 CDeal 对象，协议处理器按命令分派。配置和购买是一次请求/响应的短连接，服务端在完整响应后释放 Type2 辅助连接，避免旧对象继续占用同端点完成回调；这些连接的关闭不影响 `18000` 大厅 owner；
- `login_service.go`：登录与登录期工具消息；
- `room_dispatch.go`：房间成员和房间属性变更；
- `lobby_dispatch.go`：玩家/房间列表、创建和进入；
- `match_start_dispatch.go`：开局、地图、仲裁者与首批通知；
- `game_event_dispatch.go`：探险事件路由与发送编排；
- `match_game_over_dispatch.go`：竞技/探险 `GAME_OVER` 校验、规范化和一次性结算准备；
- `adventure_next_map_dispatch.go`：路线校验、存活者换关和关间淘汰；
- `adventure_death_dispatch.go`：NPC/玩家死亡、最终胜利与失败结算；
- `dispatch_result.go`：处理器与 TCP 发送循环的共同返回边界。
- `tcp_sequence.go`：响应、首个通知、场景切换通知和结算提交的有序发送；顺序由 TCP 帧和状态机保证，不添加未经协议或 A/B 实验证实的通用等待。网络交付失败只关闭该传输，不能取消已经形成的房间权威结算。

服务端启动也不再由 `New` 逐项发现客户端资源：`bootstrap_resources.go` 先构造不可变的地图、探险、
角色、装备、锻造、合成、商品、宠物、道具使用目录和区服文档，再单独打开 SQLite、应用种子和创建 `PlayerService`。
任何一步失败都会在监听端口建立前关闭数据库；网络运行期不重新扫描或修改这些目录。
`start-client.ps1`不承担建库、schema迁移或默认账号初始化，也不会输出默认密码；它只连接已经就绪的服务端并启动客户端/helper。

进程生命周期和账号认证能力分别由 `runtime_state.go` 的 `serverLifecycle`、`authenticationState` 持有。
两者以嵌入方式保持现有诊断字段可读，但锁的所有权已明确：监听/关闭/响应额度不能和密码挑战、跨区
导航授权共用一把锁。在线会话、对局和 UDP presence 仍是下一轮可继续收拢的热路径，不在本轮大规模
改名，以免同时破坏已经验证的实时同步测试夹具。

探险 NPC 掉落候选和墙内物品投骰已下沉到 `game/mapdata`。协议适配器只负责把领域结果验证并转换为
旧客户端 `GAME_BEGIN` 类型，数量越界或非场景 ID 会在这条边界上拒绝，而不是让协议类型进入地图规则。

原 `rooms.go` 已按职责拆为：

- `room_state_adapter.go`：建房、入房、地图/模式/角色/队伍/座位与房间属性；
- `room_inventory.go`：穿戴状态、探险快捷栏和准备道具消耗；
- `settlement_projection.go`：客户端 `GAME_OVER` 到领域结算的校验与规范化；
- `match_settlement.go`：多人结算持久化和房间恢复；全部在线成员先准备，再经应用层稳定UIN锁和持久层单一SQLite事务整批提交。大厅、糖币和背包刷新是提交后的尽力通知，不参与事务成败。
- `session_departure.go`：离房、掉线、登出和离线广播。

SQLite 的 `PlayerStore` 类型和事务语义保持不变，实现文件按 schema、凭据、库存/制作、宠物、装备和
核心档案拆开。应用层仓储契约也拆成 `ProfileRepository`、`InventoryRepository`、`CraftRepository`、
`EquipmentRepository`、`PetRepository` 五种能力，再由兼容 facade 组合；后续小服务无需依赖二十多个
无关方法。

每个协议族入口先检查集中命令号白名单，再调用对应 decoder，避免无关消息依次尝试大量结构相似的解码器。

为保持逆向调试便利，迁移没有删除 capture、原始包、结构化事件名、十六进制载荷或现有 trace 脚本。适配层另外公开 `Server.WorldSnapshot(sectionID)` 只读入口；调试代码不再直接获取私有 map 或锁，因此后续内部重构不会破坏诊断脚本。

## 对局同步模型

原客户端不是服务端逐帧重算碰撞。服务端发送 MapID、SpawnSeed、ItemSeed、参赛者和 `ArbitratePlayerID`；各客户端加载同一资源并本地模拟。仲裁客户端生成物品分布、爆炸/NPC 等关键结果。TenQQt 中的 P2P TCP/UDP/STUN 文本已定位到 `CP2PDownloader`，不能据此推断对局玩家直连。本恢复服务明确选择中心服务器验证身份/房间/对局并广播，以统一支持本机、LAN 与公网。

不同数据不能塞进同一个“广播一切”处理器：

| 数据 | 上行/来源 | 下行 | 当前状态 |
|---|---|---|---|
| 移动 | `QQT_PACKAGE_TO_SERVER 0x10EB`，内含最多255条消息；移动主体为 `0x0FA2` | `QQT_PACKAGE_TO_PLAYER 0x10E7`，绝对移动+有符号增量+普通消息 | 原版编码向量与 Go codec 已完成；外层通道、聚合、序号 ACK/纠偏未实现 |
| 放泡 | `PLAYER_USE_BOMB 0x0FA3` | `NOTIFY_PLAYER_USE_BOMB 0x138B` | Type-2 UDP唯一场景通道在中继边界完成方向转换；可靠镜像仅ACK/记账 |
| 爆炸/地图变化 | 仲裁端 `NOTIFY_BOMB_EXPLODE 0x0FA4` | 房内广播 | 静态结构已确认，实机多人待验收 |
| 拾取/使用临时道具 | `0x0FAC/0x0FAF` | `0x0FAD/0x0FB0` | 类型化并接入；账号背包不参与临时道具 |
| 击杀/救援/死亡 | `0x0FA8/0x0FAA/0x0FA7` | `0x0FA9/0x0FAB/0x0FBB` | 普通竞技/探险状态与结算已接入 |

24 字节 UDP 包已确认是 QQTPPP Type 1 rendezvous；服务端返回实际观察 endpoint，并把其 presence 绑定到已认证 TCP 会话。房间等待区实时数据使用 Type 2 的 `PlayerID+UIN` 目标表和不透明业务 Data，两个原版客户端已经完成中心中继双向同步。配置中的 P2P TCP/UDP/STUN 字符串仍属于旧下载器，不能解释 QQTPPP。LAN/公网恢复均按中心服务器开放端口设计，不依赖旧腾讯下载器 STUN，也不实现玩家间 ICE/UPnP；客户端自身直连状态机只作为后续优化。公网中心服位于家庭 NAT 后时由部署者做端口转发或使用可信 VPN；云主机开放对应安全组。

`internal/game/match.GameplayRelay` 已提供与 socket 无关的中心转发核心：只接受当前对局成员，按 PlayerID 稳定排序产生其他成员投递，按下行最多 8 条消息切批，离开或掉线移除后立即停止收发，并深拷贝可变数据。其 `DeliveryIndex` 只用于服务端诊断，明确不能在未取得证据前写入 `0x10E7.FirstIndex`。协议适配器仍待外层快路径与确认语义闭合后接入。

## 无客户端验证

运行：

```powershell
go run ./cmd/qqt-server-check
```

该命令使用临时 SQLite，实际执行账号持久化、密码挑战证明、糖币购买/背包/消耗、宠物、探险结算，以及双玩家大厅/房间/开局/结算/房主迁移/销房。它不开监听端口、不启动客户端，也不修改正式 `runtime` 数据库。需要保留诊断数据库时可传 `-database <path>`。

完整回归仍使用：

```powershell
go test ./... -count=1
go vet ./...
go build ./cmd/...
```

按项目约定不运行 `-race`。
