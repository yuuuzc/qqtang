# 协议概览

## 当前可用链路

```text
Client.exe
  ├─ TCP <server>:18080：目录请求 / RESPONSE_HALL_INFO
  ├─ TCP <server>:18000：登录、大厅、房间与离散对局事件
  ├─ UDP <server>:18000：QQTPPP Type 1/2/3/5；Type 2 承载已验收的实时房间数据
  └─ TCP <server>:18001：商城（Type3 目录连接 + 一次性 Type2 配置/购买连接）
```

`<server>` 在本机、局域网和公网模式分别由 `configs/network.json` 与启动器设置。目录响应向客户端公布同一地址；公网主机实际监听 `0.0.0.0`，但公布其配置的公网 IPv4。

客户端已经实际接受 Go 服务端响应，完成本地密码登录、频道/大厅、房间、商城、单人探险、多关、道具、结算和返回房间。`5151540001` 只是早期探针自造向量，不属于客户端协议。

## TCP 帧与游戏包络

- TCP 帧前四字节是大端总长度，包含自身四字节。
- 游戏服包络携带外层序号、路由、UIN、`1/32` 标记和 32 字节本地 ST；业务明文由本地 16 字节密钥经 QQ-TEA 保护。
- 解密后先是大端 transport command，再是方向专用 schema 对象。request/response 是否共用 command 由消息表和客户端分发表逐项确认，不能使用“响应号 +1”猜测。
- 登录为 command `0x0064`：请求 `0x03ED`，成功响应 `0x07D5`。登出为 `0x0065`；大厅、房间、聊天、商城与对局命令见 `internal/protocol/game/commands.go` 和 `docs/protocol-message-index.md`。
- 离散对局上行使用 `0x0083 + 0x03FB / REQUEST_PLAY`；服务器主动通知使用 `0x0084 + 0x041B / NOTIFY_GAME_EVENT`，两者不是同一包络。

协议 codec 校验声明长度、数组上限和字节序；畸形包返回错误而不是越界。人物、房间、背包、商城、宠物、结算和移动对象均有独立 Go 类型，不在业务处理器中手写偏移。

## 目录协议

- 目录请求和响应 transport command 均为 `0x0133`。原客户端会在新登录后按游戏端口、TLS、HTTP
  目录端口尝试同一请求；实抓确认游戏/TLS 探测使用 `Route=6 / Marker=0xFFFF / SectionID=0x00FE`，
  HTTP 回退使用相同包络但 `SectionID=0x00FD`。Go 游戏监听器直接响应这两种严格形态，因此 LAN/公网
  客户端不需要等待约 3 秒再回退到 TCP 18080；其他同号 `0x0133` 业务包不满足路由条件，不能越过认证门。
- 响应 schema `0x0816 / RESPONSE_HALL_INFO` 已由原版 `QQTEncoder.dll` 编码并反向解码验证。
- 目录同时公布游戏 TCP/UDP 18000 和商城 TCP/UDP 18001。`EnterShop` 将同一个目录端点交给 Type3 商品目录对象与 Type2 配置/购买对象；服务端按原版模型处理独立辅助连接，配置/购买响应后释放对应 Type2 连接，不改写端口，也不依赖客户端商城连接补丁。

## 对局同步与下载器 P2P 边界

- 普通离散事件（放泡、死亡、拾取、道具、换关、结算等）已有 `0x0083 → 0x0084` 中心服验证/广播路径。
- 房间等待区的移动/糖泡已确认由 UDP QQTPPP Type 2 承载：目标表为最多 8 个 `PlayerID+UIN`，服务端只验证身份并透明中继不透明业务 Data。两个原版客户端已经完成双向实机验收。
- 正式对局另有 `0x10EB / QQT_PACKAGE_TO_SERVER` 与 `0x10E7 / QQT_PACKAGE_TO_PLAYER`。原版编码向量已确认前者最多批量 255 条消息；后者含 2 条绝对移动、16 条有符号增量移动和 8 条普通消息，但它与 Type 2/TCP 可靠补发的边界仍未闭合。
- `0x0FA2 / PLAYER_MOVE` 使用 `PlayerIDs[]` 与 `MoveSeqs[]` 两段并行数组，最多 16 项。Go codec 已逐字节匹配原版 DLL。
- 客户端约在 1440 字节、最迟约 2 秒或计数 255 时刷新 `0x10EB`；这不是普通 `0x0083` 回显路径。
- `p2psvrInfo.ini` 和 TenQQt 的 P2P UDP/TCP/STUN 字符串属于 `CP2PDownloader` 连续日志簇，只证明旧资源下载器的 P2P 能力，不能作为玩家对局直连、打洞或混合传输证据。

当前恢复项目采用 Go 中心服同时支持 LAN 与公网：客户端把 Type 2 发往同一个服务器，由服务器按已认证房间身份定向中继。当前已验收路径不依赖腾讯下载器 STUN，也不要求玩家互相开放端口。客户端自身仍含尚未完全恢复的直连协商状态机，但它不是基础同步的前置条件。

## QQTPPP UDP 控制与会话映射

登录后客户端向游戏 UDP 端口发送 24 字节 Type 1 rendezvous。服务端只接受与活动 TCP 登录身份一致且来源主机匹配的 UIN/PlayerID，返回 UDP listener 实际观察到的 endpoint；未知账号不能靠 UDP 建立登录态。mode-1 presence 随认证 TCP 会话存活，不能按旧 15 秒租约清理。

Type 3 用于 peer 候选交换，Type 2 用于多目标实时业务，Type 5 与 `0x25/0x26` 属于控制/直连状态机。Type 1/2/3 已有严格头、校验和及路由测试；它们不能与 `0x10EB/0x10E7` 在没有新证据时互相等同。

## 当前未知与最小突破点

- 正式战斗中 Type 2 与 TCP 关键事件可靠补发的配对边界。
- `0x10E7.PlayerID` 的方向语义，以及 FirstIndex 与 `0x0FA2.SeqReply` 的精确关系。
- 客户端直连 state 1→2→3→5 的完整候选安装/回退语义；这是可选优化，不阻塞中心中继。

下一步应捕获一次双客户端正式对局中的 Type 2 与 TCP 可靠事件配对，再决定 `0x10EB/0x10E7/0x0FA2` 如何接入中心聚合；在此之前不把它们猜接到普通大厅 TCP command。
