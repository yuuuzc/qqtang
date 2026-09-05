# 本地一键运行

## 入口

- 双击 `start-local.cmd`：整合启动本地 Go 服务端和一个客户端。
- release 用户优先双击 `QQTang-Launcher.exe`：统一选择本机、局域网或互联网服务器/客户端并启动、停止；支持系统自带 Windows PowerShell 5.1。
- 双击 `network.cmd`：备用的独立网络设置界面。
- 双击 `start-local-multi.cmd`：整合启动服务端和两个客户端，本地账号依次为 `1000001`、`1000002`。
- 双击 `start-server.cmd`：只启动 Go 服务端。
- 双击 `start-client.cmd`：在已有服务端上增加一个客户端；第二次执行会自动启用已验证的双开兼容分支。
- 双击 `prepare-client.cmd`：只建立/校验客户端工作副本，不启动游戏。
- 双击 `stop-local.cmd`：只关闭 `runtime/run-state.json` 中路径及启动时间均匹配的全部托管进程。
- 脚本不会修改或关闭 Windows Defender，也不会启用 Windows 7 兼容模式、高 DPI 覆盖或禁用全屏优化。当前启动链不添加客户端出站规则，也不自动添加服务端入站规则。
- 开发版 PowerShell 脚本保持 Windows PowerShell 5.1 可解析：账号中文昵称由 ASCII 源码中的 Unicode 码点构造，不依赖无 BOM UTF-8 字面量的错误代码页猜测。

## 启动前校验

`scripts/start-local.ps1` 在创建进程前校验：

1. `runtime/client-patched/Client.exe`、服务端 JSON 和稳定的 `configs/sso-message-trace-stable.json` 完整 `0x1980` SSO 样本存在；启动客户端前还会验证 canonical `config/combineforge.ini`，并建立 QQTSection 实际读取且字节完全相同的 `config/combineforge.xml` 运行时资源别名；目录响应由服务端按 `directory_hall` 动态编码；
2. 网络配置中没有防火墙控制字段，启动时不安装或校验系统规则；
3. 配置地址上的 `18080`、`18443`、`17000`、`18000/TCP`、商城 `18001/TCP`、`18000/UDP` 及 GM 回环 `18100` 未被其他进程占用；商城目录与购买会话是连接同一端点的两个独立客户端 TCP 对象，不再占用 `18002`；
4. 非 `-Prebuilt` 模式下，当前 Go 工具链能构建 `qqt-server`、`qqt-launch` 和 `qqt-login-immediate`。

任一校验失败会终止启动；已经由本次脚本创建的进程会按精确路径回滚。

## 运行顺序

1. 本机/LAN/互联网主机模式启动 `runtime/bin/qqt-server-local.exe` 并等待监听；客户端模式只验证好友 `18080/TCP` 可达，不在本机启动服务端；互联网主机只启服务端；
2. 使用 `qqt-launch-local.exe -local-compat` 启动 `runtime/client-patched/Client.exe`；第二及后续实例自动增加 `-allow-multi-client`；
3. 双开兼容以 200µs 间隔等待 `Core.dll` 映射后暂停主线程，逐字节验证 `Core.dll+0x26F0` 的 19 字节签名及重定位 IAT，再只把 `Core.dll+0x2701` 的 `JNE` 改为 `JMP`。这保留 `QQTangWinClass` 互斥体，只跳过 `ERROR_ALREADY_EXISTS` 对应的激活旧窗口/退出新进程分支；商城使用原版 NetCenter 连接路径，不安装常驻商城连接补丁；
4. 探险结算只由服务端更新探险经验和拾取物；竞技胜/负/平及竞技积分保持不变，不再通过客户端内存补丁修正 UI 缓存；
5. 每个客户端先在逐字节验证的 `Client.exe+0x8C3A0` 原生 `Login` 入口安装凭据捕获；最多读取旧客户端允许的 16 字节密码，Go 取走后立即清零，不写普通日志或 capture；
6. helper 用账号随机盐、随机 nonce 和 SCRAM 风格 proof 向 Go 游戏服认证。错误密码立即返回失败并保持入口可重试，不等待旧 SSO 30 秒超时；
7. 正确认证只签发同一主机/UIN 一次、30 秒有效的正式 `0x0064` 登录许可，然后才安装已经逐字节校验的本地成功回调替换和迟到超时提示抑制；
8. helper 通过客户端原有顶层窗口的 `WM_COPYDATA (0x1980)` 路径触发回调；客户端随后自然发送 `0x0064`，服务端消费一次许可并从 SQLite 返回人物/背包；
9. 本地人物进入大厅后，登录组件只完成已证明的教程已完成阶段 `1 -> 2`，避免“正在读取房间信息”门槛。背包和探险卡完全由 SQLite/正式 `RESPONSE_LOGIN` 提供，不再写死到 helper；
10. 成功回调替换和超时抑制在客户端存活期间保持安装，用于吸收旧 SSO 稍后送达的重复终态回调；凭据捕获记录和密码切片已经清零。

启动脚本不再读取带实验编号的 `runtime/logs/sso-message-trace-v111.json`。稳定文件只保留登录组件实际消费的 `copy_data_id`、长度和 571 字节载荷；其载荷已经与 v111 逐字节复核一致，原追踪文件仍作为证据保留。

双开兼容不加载参考补丁中的第三方 `WSOCK32.DLL/WSOCK33.dll`，不修改磁盘上的 `Core.dll`，也不关闭 Windows 安全功能。登录触发仍走客户端的 GUI 线程和原有消息分发链。目录网络端点由 `configs/network.json` 驱动；本机、RFC1918 私有 IPv4 和用户配置的公网 IPv4 分别用于 local/LAN/remote 模式，显示层级保持 GBK 编码的“东部电信 / 自由频道 / 自由1区”。

## GM 与局域网

- 启动器“打开 GM”默认访问 `http://127.0.0.1:18100/gm/`。页面不依赖 Node，可管理账号、人物和背包。
- 启动器可设置 GM 用户名/密码；只保存 PBKDF2 加盐哈希。GM 非回环监听必须有有效凭据，公网访问还应使用 HTTPS 反向代理。
- 网络设置同时更新 `configs/network.json` 和工作副本中的 `DirCfg.ini`、`p2psvrInfo.ini`、`caserver.ini`、`webserver.ini` 13 个地址键。
- 详细端口、远程绑定与安全边界见 `docs/network-deployment.md`。

## 状态与日志

- 运行状态：`runtime/run-state.json`
- 服务端标准输出/错误：`runtime/logs/local-server.stdout.log`、`local-server.stderr.log`
- 客户端启动结果：`runtime/logs/local-client-launch.json`
- 即时登录就绪标记：`runtime/logs/local-login-immediate.ready.json`
- 即时登录最终记录：`runtime/logs/local-login-immediate.json`
- 启动失败记录：`runtime/logs/start-local-error.log`

第二及后续实例的日志在扩展名前增加编号，例如 `local-client-launch-2.json`、`local-login-immediate.ready-2.json`。`runtime/run-state.json` schema 3 使用 `clients[]`、`login_helpers[]` 和 `accounts[]` 保存全部实例，同时保留指向第一实例的旧兼容字段。

`ready.json` 的 `stage` 为 `armed` 表示可以输入本地账号密码并点击登录；`auth_rejected` 表示密码已被立即拒绝且可以重试；`login_replayed` 表示认证通过后本地成功回调已经发生；`lobby_prepared` 表示大厅阶段已完成。结果只记录认证 UIN、次数和错误摘要，不记录密码、盐、nonce 或 proof。
