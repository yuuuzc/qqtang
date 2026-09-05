# 多客户端实时同步：唯一当前结论

更新时间：2026-08-16（Asia/Shanghai）

本文件是房间内移动、放糖泡和后续对局快路径的唯一当前入口。继续开发时先读本文件，不得从旧日志、旧代码注释或单元测试重新把已失败方案升级为候选。

## 知识优先级与清理规则

1. 当前代码、自动测试和最后一次原版双客户端实机结果优先级最高。
2. 本文件只保存仍然成立的联机结论、明确未知项和永久排除项；它覆盖 `evidence/` 中同主题的历史阶段判断。
3. `evidence/` 保留实验发生时的原始结论用于审计。被后续证据推翻的条目必须带“后续更正”，不能重新当作当前方案。
4. `runtime/analysis`、`runtime/logs` 和临时 tracer 只是原始证据定位，不是知识入口；没有同时写入本文件的临时猜测不得进入实现。
5. 房间等待区和正式对局的 Type 2 实时同步已经完成；客户端直连只是一项可选优化，不能把它重新描述成基础联机前置条件。

## 已确认的活动路径

- 两个原版客户端可以登录同一大厅、加入同一房间；准备、取消准备、角色和队伍等普通房间消息可跨连接同步。
- 服务端实时并发采用房间 Actor 边界，而不是连接互锁：World/Room/Battle 是权威状态；每条连接只短暂串行自己的请求，广播和网络写在解锁后执行。开局、换关、返回房间和踢出状态经目标会话邮箱投影，任何普通实时路径都不得阻塞获取另一玩家的 `session.mu`。完整约束见 `evidence/server-room-actor-lock-model-v1.md`。
- 房间中的移动和放糖泡走独立快通道。客户端上行外层 ST 为 `01 05 0065 01 <目标PlayerID>`；解密后是 compact `QQT_MSG_DATA` 批次。已观察事件：`0x0BB8 ROOM_PLAYER_MOVE_INFO`、`0x0BB9 ROOM_PLAYER_PUT_BOMB`。
- 24/30 字节 UDP 控制包的活动实现属于 `QQTPPP.dll`，不是此前误判的 NetCenter：
  - Type 1（24 字节）：双向 endpoint rendezvous；客户端请求携带本地 endpoint，服务端响应保留同一包头身份/序号但把 endpoint 改成 UDP 监听器实际观察到的来源地址；
  - Type 3（30 字节）：payload 先指定目标 PlayerID/UIN，随后公布外层 header 所标识发送者的候选 endpoint；服务端按目标身份定向交付，并可把不可信候选改写为实际观察到的已认证来源；
  - Type 2：1..8 个目标身份（每项为网络序 `PlayerID(2)+UIN(4)`）加不透明业务载荷，完整构造包不得超过 1024 字节；这是已经双向实机闭环的实时房间/对局 UDP 数据入口；
  - Type 5：mode 1 的空载控制包；mode 0 另有直接发往 secondary endpoint 的 `0x25/0x26` 帧。
- 活动客户端内同时存在两个 QQTPPP 通道对象：mode 0 与 mode 1。二者均持有当前 UIN/PlayerID 和独立 UDP socket；mode 0 构造 Type 3/`0x25`/`0x26`，mode 1 构造 Type 1/Type 2/Type 5。
- QQTPPP 公共 18 字节头已经由当前 5.2 构造器逐字段闭合：总长度、flags、32 位递增包号、PlayerID、UIN、Type、校验和、地址表字节数。校验为对整包按小端 16 位字求和、折叠进位并取反，结果以网络序写入；完整有效包再次计算结果为 0。
- `QQTPPP!FUN_10006704` 已静态证明 Type 2 路由表循环对每个目标执行 `htons(*param_3)` 与 `htonl(*param_4)`；2026-08-12 自然线上帧的唯一条目逐字节为 `0001 000F4241`，即糖一的 `PlayerID=1/UIN=1000001`。旧实现把一次恰好呈 endpoint 外形的人工样本误命名为 `Port+IPv4`，导致自然包被解析成荒谬的 `0.15.66.65:1` 并拒绝；该解释和 codec 已永久修正。
- Type 2 路由时只解析公共头与目标身份，地址表后的 Data 保持不透明并逐字节转发。自然帧中的该段会经过客户端原生编码接口，Go 中心服不解密、不改写，也不重新生成校验范围内的业务字节；这样同时兼容房间移动、糖泡和后续未知事件。
- 2026-08-12 双开实机已完成双向闭环：糖二→糖一 9 包、糖一→糖二 6 包均记录为 `room_peer_udp_multicast_routed`，原版接收端分别实际显示移动和糖泡，零拒绝。包长覆盖 80/104/112 字节。基础多人实时同步不再是阻塞项。
- 2026-08-19 TP-free兼容层回归已闭环：`CreateObj(3)`的发送slot 9成功值为0，接收slot 11成功值为1。QQTModules把接收底层非零成功转换成外层0，QQTPPP只在外层0时继续向peer/QQTSection投递；旧版把解码成功也返回0，包虽已完整解码却立即释放。修正后两个TP-free客户端实机恢复远端移动显示。该问题位于客户端历史变换ABI，不改变本页已经闭合的Type 2身份路由、Type 1 presence或Go透明中继，详见EVID-273。
- 2026-08-28 源码复核撤回了物品通知跨通道投影：`Client FUN_00603F01` 消费 `0x0FAE` 时，会用每个延时项的 `(ItemID, DispatchTime)` 清理前置飞鸟事件创建的本地待投放记录；`FUN_00604DB0` 消费 `0x0FAD` 时，又要求当前场景已存在同格同 ItemID 的对象。两者都不是可脱离前置 Type-2 状态独立执行的最终事实。Go 不再拆分、重编码或改走 `0x0084`，也不把 `0x0065` 镜像延时补发成第二份 Type-2；完整原始密文批次按原 packet number、batch/message index、顺序和通道逐字节转发。虚拟 AI 镜像同样只消费 Type-2，且非移动事实在真人下行之后提交，避免可靠镜像抢先让 AI 进入香蕉 Rate 10 等依赖场景状态。
- 2026-08-16 正式对局闭合三路关系：客户端原生场景只由QQTPPP Type-2经Go中心服转发；`0x0065`和`0x0083`是同一动作的可靠校验/记账镜像，不是TCP场景兜底。直连协商或NAT打洞失败不影响中心转发，但所有客户端必须能向中心服UDP监听端口发包并接收响应；若中心UDP本身被防火墙完全阻断，当前原生场景通道不会自动降级为TCP。
- 探险多关必须同时维护两层身份：`CurrentGameID` 是整场探险的稳定 MatchID，供房间、战斗状态和最终结算使用；`CurrentStageGameID` 是客户端当前关卡的 wire GameID。进入下一关时只分配并广播新的 StageGameID，实时 Type 2/TCP 快路径按 StageGameID 鉴权，不能覆盖稳定 MatchID。2026-08-13 双客户端已实机验证第二关仍能互见移动，退出也能广播。
- 服务端主动通知不能继承接收者最近请求包里的 request-only 内层调度字段。`RebindLocalServerNotification` 现在统一把 `InnerUnknown`、内层 sequence 和 route sequence 清零，仅保留接收者信封身份及通知自身的业务 route/section。旧实现把糖二普通 `0x00A7` 请求的 `InnerUnknown=0x00020000` 带入第二关 `GAME_OVER`，客户端随后自行发出 `0x0069 LeaveRoom`；修正后双客户端已实机验证第二关失败结算均回到原房间，首关行为不受影响。
- Type 1 presence 的生命周期也已实机纠正：客户端收到观测 endpoint 后停止周期 Type 1，因此 mode-1 presence 必须随已认证 TCP 会话存活，不能和每 3 秒刷新的 mode-0 Type 3 endpoint 共用 15 秒短租约。旧实现正是在进房约 42 秒后把合法 Type 2 误拒为 `no current Type-1 presence`；现在仅 mode-0 记录按 15 秒清理，断开最后一个同 UIN 主会话时清除两类记录。
- 双端控制器内存均包含本地与远端 UIN；本轮成功同步时 peer 仍为 state 1、candidate A/B 仍为空。这证明中心服 Type 2 路由不依赖直连协商进入 state 5；state 2/3/5 只作为后续恢复客户端直连/降流量路径继续研究，不能再阻塞基础联机。
- `+0x25E20/+0x25E24` 必须以 QQTPPP 主接口子对象 `this=owner+0x20` 为基址解释，不能与 owner 或内嵌 transport 的同号偏移混用。活动对象中 `this+0x25E20` 正好是十进制 `18000`，前面依次是本地 UIN、PlayerID 和服务器 `127.0.0.1`；本轮两端 `this+0x25E24` 都正确显示为 `127.0.0.1`。它既不是 ready 布尔，也不是 transport 结果码，而是 Type 1 服务端响应返回的观测 IPv4。
- 这条语义已由构造、活动内存和接收分支三方闭合：owner 在 `+0x259E8` 内嵌 transport，并把主接口 `owner+0x20`（vtable `1000D214`，slot 0=`FUN_10003B99`）写入 transport `+0x448` callback。transport `+0x428/+0x42C` 的活动值是服务器 IPv4 `127.0.0.1` 与端口 `18000`，并非 socket handle/result。Type 1 接收分支解析响应中的 IPv4 后调用 transport slot 1；slot 1 只接受来源 IPv4 等于已配置服务器 IPv4的 datagram，并把响应载荷中的 IPv4 传给 main slot 0。main slot 0 将其原样保存到 `this+0x25E24`。按 Winsock 网络序 dword 保存后，内部 getter 读取低字节也正好得到 IPv4 第一段。旧 Go 服务端只登记客户端 Type 1 presence、从不响应，活动抓包也证实服务器对周期 Type 1 零下行，因此该字段长期为 0。
- Go 服务端现已实现经过活动 TCP 会话身份校验后的 Type 1 观测地址响应：保留请求的 flags、包号、PlayerID 与 UIN，仅用 UDP listener 观察到的来源 IPv4/port 替换 endpoint，重新编码校验和后回给同一来源；未认证请求不回包，避免形成反射器。聚焦测试覆盖响应来源端口、身份/包号保持、观测 endpoint 替换及后续 Type 2 顺序。
- Client 房间层对象的 `+0x30C` 是通过 QQTPPP IID `9F16F213-2E6F-4EB1-AFE1-E59B64754768` 获取的正式发送接口；发送 `0x0BB8/0x0BB9` 时明确调用该接口 `vtable+0x24(dataID, payload, routeType=2)`。接口 thunk 回到 QQTPPP 根对象 slot 27，随后按最多 8 个 active peer 构造 Type 2；因此发送端对象、IID、routeType 与 wire Type 2 已完整闭合。
- 已从真实双开会话捕获 TCP `0x0086 ID_CMS_REQUESTUDPOK`：schema 为 `0x0411 REQUEST_TRANSFER_UDP_OK`，字段依次为 `Uin / Time / DstDlg / DstUin / InfoLength / Info`。样本是糖二 `1000002/PlayerID=2` 发给糖一 `1000001/PlayerID=1`，`Info=00`。服务端不能原样广播该上行，必须按目标连接信封构造直接 `0x0087 ID_SMC_NOTIFYUDPOK`，并把主体转换成 `0x0413 NOTIFY_UDP_OK`：目标 UIN、原 Time、来源 PlayerID、来源 UIN、原 Info。
- `0x0087` 的客户端消费路径已静态闭合。QQTSection 消息子对象的 vtable 位于根对象 `+BF30`，其 slot 69 为 `FUN_100220D8`；该函数按 `NOTIFY_UDP_OK` 的精确字段偏移读取 `SrcDlg(+8) / SrcUin(+10) / Info(+16)`。`Info[0]==0` 时调用保存接口的 `vtable+0x30(SrcDlg, SrcUin)`；非零时从 `Info+1` 读取一个网络序 32 位值、从 `Info+5` 读取一个网络序 16 位值，再调用 `vtable+0x34(SrcDlg, SrcUin, AddressValue, AddressTag)`。
- 上述保存接口并非相邻字段猜测：`FUN_10018E3A` 把 slot-69 vtable 写到根对象 `+BF30`，所以 handler 的 `this+61C` 精确等于根对象 `+C54C`；`FUN_10023CCC` 又使用 IID `9F16F213-2E6F-4EB1-AFE1-E59B64754768` 查询 QQTPPP 并写入 `+C54C`。QQTPPP `vtable+0x30/+0x34` 分别进入共享接口 slot 12/13，再转到根 slot 30/31。
- QQTPPP 根 slot 30 按 `SrcUin` 在最多 8 个 peer 中定位对象，并调用 `FUN_10004E0F`：非状态 3/5 的 peer 被置为状态 2；已有候选传输地址时立即启动下一步。根 slot 31 同样按 UIN 定位 peer，把通知携带的 32/16 位地址对交给 `FUN_1000505B`；该分支只在 peer 状态 3 时安装候选地址并转到状态 5。由此 `Info=00` 是正式的 peer negotiation 触发，不是 QQTPPP 全局 ready 位。
- Type 3 的控制器入口现已精确定位为主接口 slot 2=`FUN_10003BBD`。它按 Type 3 外层发送者 UIN 查找八个 peer，把 transport 实际来源与 payload 中的发送者候选交给 `FUN_10004E99`；若 peer 尚未创建，则把两组地址缓存到 `this+0xF857C` 的 32 项延迟表，`AddPeer/FUN_1000358A` 创建对应 peer 后再取出并删除。候选安装完成后，控制器通过 QQTSection callback slot 5 发送 `Info=00` 的 `0x0086`。这条链排除了“Type 3 目标 endpoint”“候选未进入 peer”和“必须先有业务 Type 2 才开始协商”三种旧解释。
- Type 3 不是一次性登记包。历史双端日志显示双方在协商期间约每 3 秒重发；目标 endpoint 尚未登记时记录为 `target_*_pending`，另一端出现后的后续轮次自然转为双向 routed。因而 Go 不需要保存并补发首个 pending 包；额外队列反而会改变原客户端的重试/包号时序。
- 自然直连握手状态机已静态闭合：目标收到 `Info=00` 的 `0x0087` 后，peer 从 state 1 进入 state 2；只要候选 A 非零，`FUN_10004E0F` 就调用 `FUN_10004E50(peer,0x20)`，设置 state 3、三次重试并从 mode-0 直连 socket 发送 15 字节 flags=`0x20` 帧。收到对应协商结果后，QQTSection 转发的非零 `0x0087` 进入根 slot 31，`FUN_1000505B` 只在 state 3 接受地址对并切到 state 5。但本轮实机已证明经中心服投递的 mode-1 Type 2 在 state 1 也能被应用，直连状态机不是中心中继的前置门槛。
- QQTPPP 接收端后半链也已定位到正式对象边界。主接口 slot 1 的 vtable 项是 `1000154B -> FUN_100056AB`；旧分析把相邻的 `10001550 -> FUN_10004143` 误当成 slot 1，后者已永久排除。真实 Type 2 动态跟踪证明 `FUN_100056AB` 按来源 UIN 找到 peer，并调用 peer vtable slot 3；参数仍包含来源 UIN、业务指针和业务长度。旧双开样本进入 peer slot 3 时该 peer 的 `+0x460` 状态仍为 `1`，处理前后只把重试计数 `+0x448` 从 `2` 减到 `1`，没有进入上层业务回调。
- 每个 peer 的 `+0x43C` 精确指向 owner 的内部子接口 `owner+0x24`，vtable 为 `1000D1EC`，不是 QQTSection 回调对象。该内部接口 slot 1/2/7 分别转发到 `owner+0xA4` 保存的 QQTSection callback slot 6/7/8。活动 callback vtable 为 `QQTSection+0x54870`；slot 6 继续到 Section owner slot 28，slot 7 继续到 owner slot 29，再进入 `owner+0xC558` 房间消息分发器。`FUN_10004FFB` 已明确在收到 flags `0x20` 时调用内部 slot 1，这是协商/endpoint 事件；内部 slot 2 的参数形态与 Type 2 业务投递吻合，是协商完成后需要观察的正式业务入口。旧样本对 `owner+0x24` 的透明 vtable 跟踪为 0 次，与 peer 尚停在 state 1 一致，不能解释为业务接口不存在。
- 非零分支也已有真实样本：糖一发给糖二的 `Info=01 000F4242 0003`，其中地址对数值恰为目标 UIN `1000002` 和 `3`，不能命名成物理 `IPv4:port`。发送端 `NetCenter!FUN_10018ABC` 明确写入前缀 `1`，再从目标 peer 记录 `+0x14` 复制由 `+0x94` 指定长度的 opaque transport descriptor；本样本 descriptor 长 6。恢复服务只验证结构完整并透明转发，不解释或改写该地址对。
- 旧抓包中的首个 15 字节 flags=`0x21` 确实由人工 Type 3 flags=`0x20` probe 触发，不能冒充自然运行证据；但新的静态链已经证明自然零模式 `0x0087` 会经 `FUN_10004E0F→FUN_10004E50(peer,0x20)` 生成同族直连握手。帧布局为 `Length(2) / Flags(1, 0x20或0x21) / Checksum(2) / SenderUIN(4) / IPv4(4) / Port(2)`，校验仍为 QQTPPP 16 位反码和。帧中没有目标 UIN，正常情况由客户端直接发往某个已选 peer 候选；若候选退化为 rendezvous server，服务端不能猜单一目标，只能在严格验证发送者 UIN、mode-0 来源端点和房间成员后向该房间其他已登记 mode-0 端点广播。Go 已加入这一受限中继兜底，正常 P2P 直连不经过它。
- QQTPPP 主接口 slot 0 保存 Type 1 服务端响应中的观测 IPv4 后调用 QQTSection callback slot 3；slot 3 定位 QQTSection owner、调用 owner slot 25，使 `+0x1A0` 完成计数加一。计数到 2 时，它通过 IID `5CD2F2AF-D955-4C44-B9C3-B744A464D604` 获取 QQTModules 接口并调用 `vtable+0x34(SectionID, SubsectionID, 0, 6, 0)`。QQTModules 缓存参数并 `PostMessageA(hwnd, 0x402, 6, 0)`；`0x402` handler 不按 `wParam=6` 分支，只向最多 128 个订阅者广播五个缓存参数，当前活动对象订阅表为空。因此 Type 1 rendezvous 会间接推进这一初始化计数，但 `0x402/wParam=6` 本身仍是通用模块完成事件，不发送 `0x0086/0x0087`，也不是房间业务包入口。
- 当前 `runtime/bin/qqt-server-local.exe` 已包含 Type 1 响应、会话级 mode-1 presence、Type 2 身份路由、`0x0086→0x0087` 转换和 15 字节握手受限中继；Type 2 双向同步已于 2026-08-12 用两个原版客户端实机验收。活动构建 SHA-256 为 `21AF060E0568C8597CB27AB6DD43F68D42E331EF430A0AA1256FB3CA2B4DB088`。
- `QQTangCheatEngine` 的旧版源码提供独立交叉证据：`CSendPackageInRoom` 包含 GameID、Length、SendCount 和载荷；房间移动/糖泡分别为 `0x0BB8/0x0BB9`；`SetDataToClientInRoom` 编码后以 `PackageType=2` 交给房间网络对象。其对象布局也与当前 5.2 QQTPPP 字段吻合。旧偏移和注入代码不进入正式实现，只用于命名与结构复核。

## 已明确排除，禁止重复

| 方案 | 实机结果 | 结论 |
|---|---|---|
| 把 `0x0065` 快通道上行帧只改外层 UIN 后，经普通 TCP 连接原样发给接收者 | 服务端实际交付 79/87 字节，接收者无移动、无糖泡 | 上行帧不是可直接消费的 TCP 下行帧 |
| 把上行中的 compact `BatchData` 封装为普通 `0x009D NOTIFY_TCP_TRANSFER`，Route 3 / Section 1 | 多轮实际交付 122/130 字节，接收者无移动、无糖泡 | 普通 QQTSection `0x009D` 不是该房间快通道的有效接收入口 |
| 在客户端主线程直接调用推定 `CMySection::OnRecvP2pTcpData`，传入同一批次 | 调用线程超时，返回/计数没有证明远端移动或糖泡被应用 | 入口、对象或调用约定未闭合，不能作为实现 |
| 把反编译中连接类型 3 的“9 个调用参数”解释成 9 个可逐一尝试的消息槽 | 当前构造器证明 `param_5 < 9` 是 Type 2 的目标身份数量限制，即最多 8 人 | “9 消息槽”已排除，不再逐槽注入 |
| 用 `p2psvrInfo.ini` 的 P2P TCP/UDP/STUN 字符串解释对局传输 | 静态交叉引用位于 `CP2PDownloader` 下载诊断簇 | 这些字符串不是对局 P2P 证据 |
| 把 NetCenter 的 10 字节 relay envelope 套在活动 Type 1/3 上 | QQTPPP 接收器直接取得原始 UDP payload 与 transport source endpoint | 该 envelope 属于另一条 NetCenter 路径，不是当前活动链 |
| 服务端尝试解析、重编码或修改 Type 2 地址表后的业务段 | 自然线上段经过客户端原生编码接口；服务端逐字节透明转发后双端已实际显示移动/糖泡 | 中心服只验证公共头和目标身份，业务 Data 必须保持不透明 |
| 认为接收端 QQTPPP 没登记远端 UIN | 双端控制器内存均同时存在本地与远端 UIN，Type 2 还按远端 UIN进入了控制器 slot 1 | 玩家身份注册已完成；只追控制器后的应用回调 |
| 把 Type 2 的六字节目标项解释成 `Port+IPv4` | 自然帧为 `0001 000F4241`，构造器逐项执行 PlayerID/UIN 的 16/32 位网络序转换；身份修正后立即双向同步 | 目标项只能命名为 `PlayerID+UIN`，实际 UDP 目的地址由服务端的 Type 1 presence 映射取得 |
| 认为 `+0x25E24` 是 ready/失败/结果码，或应固定置为 `1` | transport slot 1 的首参实际匹配已配置服务器 IPv4，次参来自 Type 1 响应 endpoint 的 IPv4；main slot 0 将次参原样保存 | 该字段是服务端观测 IPv4；`0` 表示旧服务端尚未返回 Type 1 rendezvous 响应，必须回真实观测地址而非魔法值 |
| 把 `0x0086` 当作 UDP Type 3，或把其上行主体直接回送 | 原始 schema 是带目标身份的 `REQUEST_TRANSFER_UDP_OK`；客户端接收入口需要带来源身份的 `NOTIFY_UDP_OK` | 必须由服务端执行 `0x0086/0x0411 → 0x0087/0x0413` 定向转换 |
| 把 15 字节 `00 0F 21 ...` 当成服务端应直接回答的注册请求 | 帧只有 SenderUIN 和一个地址对，没有目标身份；自然发送点位于 peer 的 mode-0 直连状态机，旧首帧虽由人工 probe 触发，静态链仍证明零模式 `0x0087` 会自然发送同族 `0x20` | 服务端不伪造 `0x21` 应答；仅在帧实际到达服务器时，对同房已认证 mode-0 peers 做受限中继兜底 |

`0x009C/0x009D` 的静态恢复结果只作为取证记录保留，不进入当前运行时代码；历史编解码自洽也不能推翻上述实机排除结论。

## 历史资料如何使用

- 有 QQTang server 经验的历史文章称：所有游戏事件经 UDP 转发，拾取、泡泡爆炸等关键事件还会用 TCP 再发一次；QQTang 当年的 P2P 打通/流量比例约 70%，采用事件即时转发而不是周期状态快照。
- 2005 年抓包分析观察到进入地图前只有 TCP、进入地图后 UDP 占主导、UDP 对端数量与房间玩家一一对应，并推断移动由玩家间 UDP 广播。
- 这些资料与 Type 2 多目标 UDP 构造器高度一致，但仍只作为旁证；当前 5.2 二进制、活动抓包和校验向量优先级更高。

## 仍未闭合

1. 客户端直连优化仍未闭合：本轮 Type 3 双向路由持续发生，但 peer candidate 仍为空且 state 1。它不影响中心服 Type 2 同步，后续只在优化公网带宽/时延时继续追。
2. 非零 `Info` transport-address 分支已有 `01 + UIN + 3` 样本，但 `AddressTag=3` 的底层逻辑/中继语义仍未完全命名。Go 透明保留该 blob，并按客户端读取行为拒绝少于 `mode+32-bit value+16-bit tag` 7 字节的畸形值。
3. 服务端主动生成 Type 2 时仍需恢复客户端原生业务编码接口；当前已验收的是客户端生成、Go 透明中继的主流实时路径。
4. `0x10EB -> 0x10E7`是否另有服务端纠偏用途仍可继续命名，但不能再把`0x0065/0x0083`可靠镜像当成Type-2失败时的场景补发。

## 下一条允许推进的路径

1. 保留已经实机验收的 Type 1 会话映射、Type 2 身份 codec 与中心透明中继；服务端不得改写不透明 Data。
2. 客户端直连 state 1→2→3→5 仅作为降低中心带宽/时延的可选优化，不能回退或替代已工作的中心中继。
3. 不再测试本节列出的失败注入、错误 endpoint 解释、原样 TCP 下发或“9 槽”枚举方案。

## 证据定位

- 当前 5.2 QQTPPP 构造器：`runtime/analysis/qqtppp-send-constructors-v51.txt`
- 原生 Type 2 线上帧及接收端 raw sink：`runtime/analysis/qqtppp-sugar1-triggered-type2-trace.json`
- Type 2 已进入控制器及解析后 payload：`runtime/analysis/qqtppp-sugar1-controller-trigger-v58.json`、`runtime/analysis/qqtppp-sugar1-controller-payload-v69.json`
- 双端控制器包含双方 UIN 的快照：`runtime/analysis/qqtppp-client1-controller-post-lan-v78.bin`、`runtime/analysis/qqtppp-client2-controller-post-lan-v78.bin`
- 公共头与校验和：`runtime/analysis/qqtppp-checksum-and-init-v52.txt`
- 对象字段引用：`runtime/analysis/qqtppp-object-offset-uses-v50.txt`
- QQTPPP UDP callback：`internal/tooling/winlaunch/qqtppp_udp_callback_trace_windows.go`
- Client 正式 QQTPPP 发送接口：`runtime/analysis/client-offset-30c-v92.txt`、`runtime/analysis/qqtppp-send-interface-v93.txt`、`runtime/analysis/qqtppp-controller-room-methods-v96.txt`
- Type 1 rendezvous callback 与 QQTModules 异步通知：`runtime/analysis/qqtsection-ready-handler-v100.txt`、`runtime/analysis/qqtsection-ready-owner-handler-v102.txt`、`runtime/analysis/qqtsection-ready-followup-v103.txt`、`runtime/analysis/qqtmodules-ready-root-handler-v107.txt`
- Type 1 接收、transport callback 与 `0x402` 广播闭环：`runtime/analysis/qqtppp-transport-vtable-v120.txt`、`runtime/analysis/qqtppp-transport-callback-methods-v119.txt`、`runtime/analysis/qqtppp-asyncsocket-events-v127.txt`、`runtime/analysis/qqtmodules-message-402-handler-v109.txt`、`runtime/analysis/qqtmodules-consumers-live-v121.bin`
- 根对象到内嵌传输 callback 的构造绑定：`runtime/analysis/qqtppp-dispatch-interface-registration-v210.txt`、`runtime/analysis/qqtppp-controller-vtable-v56.txt`
- Type 2 主接口/peer 调用链：`runtime/analysis/qqtppp-type2-path-v147.json`、`runtime/analysis/qqtppp-peer-type2-v151.json`、`runtime/analysis/qqtppp-peer-before-v159.bin`、`runtime/analysis/qqtppp-peer-after-v159.bin`
- peer 内部子接口与 QQTSection callback：`runtime/analysis/qqtppp-controller-subinterface-v88.txt`、`runtime/analysis/qqtppp-mode1-callbacks-v54.txt`、`runtime/analysis/qqtsection-qqtppp-callback-full-v97.txt`、`runtime/analysis/qqtsection-interface-construction-v234.txt`
- 真实 `0x0086`：`runtime/captures/directory-local-ui/session-20260811-141152.230923700/capture.jsonl`（非零 descriptor line 34414，零模式 line 35569）
- QQTSection `0x0413` handler 与 QQTPPP 接口来源：`runtime/analysis/qqtsection-indirect-slot30-callers-v228.txt`、`runtime/analysis/qqtsection-interface-construction-v234.txt`、`runtime/analysis/qqtsection-p2p-iid-bytes-v235.txt`
- QQTPPP slot 12/13 与 peer 状态迁移：`runtime/analysis/qqtppp-shared-interface-callback-family-v221.txt`、`runtime/analysis/qqtppp-root-udp-ok-branches-v237.txt`、`runtime/analysis/qqtppp-root-udp-ok-transition-details-v238.txt`
- 非零 transport descriptor 的生成与保存：`runtime/analysis/netcenter-udp-ok-sender-v191.txt`、`runtime/analysis/qqtppp-udp-ok-nonzero-semantics-v241.txt`、`runtime/analysis/qqtppp-address-pair-container-v242.txt`
- 人工 flags=`0x20` Type 3 probe 与 `0x21` 重试来源：`runtime/analysis/qqtppp-type3-op20-probe-v179.json`、`runtime/analysis/qqtppp-type3-op20-route-v179.json`、`runtime/captures/directory-local-ui/session-20260811-141152.230923700/capture.jsonl`（line 34414/34415）
- Go `0x0086→0x0087` 强类型转换：`internal/protocol/game/udp_ok.go`、`internal/server/probe/peer_transport_dispatch.go`
- Go 严格控制包 codec：`internal/protocol/game/room_peer_udp.go`
- Go 同房鉴权、Type 3 路由及 15 字节握手受限中继：`internal/server/probe/room_peer_udp.go`
- 旧版源码参考：[`blackmaple/QQTangCheatEngine`](https://github.com/blackmaple/QQTangCheatEngine) commit `251fda6cd6bc4b4ece1939623272a5ba175dee3e`；只读临时克隆已在完成结构交叉验证后清理
- compact 快包：`internal/protocol/game/room_fast_packet.go`
- 原版编码向量和正式对局对象：`evidence/gameplay-fast-path-vectors-v1.json`
