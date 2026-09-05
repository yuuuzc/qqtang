QQ堂怀旧版（Windows 10 / 11 64 位）

本项目永久免费。请勿付费购买；只从以下公开项目获取更新：
https://github.com/kuuhaku1314/qqtang

开始游玩

1. 完整解压整个 QQTang-Local 文件夹，不要直接在压缩包中运行，也不要覆盖旧版本。
2. 双击 QQTang-Launcher.exe。
3. 在“服务端”区域选择“仅本机”，点击“启动服务端”。
4. 在“游戏客户端”区域保持 127.0.0.1，点击“启动一个客户端”。
5. 使用本地测试账号 1000001、初始密码 123456 登录，不要输入真实 QQ 密码。
6. 结束时分别点击“停止所有客户端”和“停止服务端”。

TP 已完全移除

- 发布包中的 Client.exe 已重建为普通 PE 启动链，不再启动或依赖旧版腾讯 TP。
- 原版 ClientBase.dll、TerSafe.dll、TenSLX.dll、TP3Helper.exe 和 TesSafe.sys 均未随包分发。
- 包内同名 TerSafe.dll 是本项目生成的 2 KB 无保护协议 ABI 兼容层，只补齐老客户端模块仍会调用的对象和数据变换接口；它不包含原版 TP 代码，不进行安全扫描，也不会启动保护服务。
- 启动游戏不会安装、注册或加载 TP 内核驱动，也不需要关闭 Windows 安全功能。

无界面服务端

发布包同时提供三个控制台启动脚本：

- Windows 64 位：start-server-windows.cmd
- Linux AMD64 / x86_64：start-server-linux-amd64.sh
- Linux ARM64 / aarch64：start-server-linux-arm64.sh

Windows 双击对应 cmd 即可。Linux 先进入完整解压后的 QQTang-Local 目录，然后运行：

  chmod +x start-server-linux-*.sh
  ./start-server-linux-amd64.sh

ARM64 机器把最后一行改为 ./start-server-linux-arm64.sh。脚本在前台运行服务端，按 Ctrl+C 即可停止；systemd、Docker 等托管程序也可直接向进程发送 SIGTERM。

无界面模式读取 configs/network.json：

- 仅本机：mode 为 local，server_ip 与 client_server_ip 均为 127.0.0.1。
- 局域网主机：mode 为 lan-host，两个 IP 填主机的私有 IPv4。
- 公网服务器：mode 为 remote-host，server_ip 与 client_server_ip 可填写玩家能够访问的公网 IPv4 或域名。域名会在启动/写入客户端配置时解析为 IPv4 A 记录；目录协议仍下发原生 4 字节 IPv4。
- 建议保持 gm_remote 为 false；如需远程管理，请先设置强密码并使用可信反向代理或 SSH 隧道。

Linux 服务端仍需要发布包中的 configs、data 和 runtime/client-patched 静态配置，不能只复制单个二进制。存档位于 runtime/data/qqtang.sqlite；升级或迁移前请自行备份该文件。日志位于 runtime/logs。

Linux AMD64 与 ARM64 包内分别携带对应架构的 ONNX Runtime .so；启动脚本会选择本架构配置和动态库。不要在两种架构之间混用 runtime/onnxruntime 目录。

多开

- 重复点击“启动一个客户端”即可多开，实例和本地测试账号会自动顺延。
- 第二个默认测试账号是 1000002，初始密码也是 123456。
- 启动器不设置固定客户端数量上限；实际可运行数量取决于电脑资源，房间人数服从具体地图上限。
- 不要安装网上来源不明的多开 DLL 或补丁。

局域网与公网联机

- Windows 图形启动器可直接选择“局域网”或“公网部署”并保存公布 IP。
- 服务端监听 TCP 17000、18000、18001、18080、18443 和 UDP 18000。
- 云服务器安全组、主机防火墙或家庭路由器需要由服务器主人自行放行这些端口。
- 客户端只需填写服务器 IP，不需要自行做端口映射。
- 启动器和脚本不会自动新增防火墙规则、提权或关闭安全功能。

GM 后台

- 默认本机地址：http://127.0.0.1:18100/gm/。
- 本机访问不需要密码；远程开放 GM 前必须设置独立的 GM 密码。
- 默认用户名为 admin，密码仅以加盐哈希保存，不保存明文。
- 使用控制台脚本时，先把 configs/network.json 的 gm_remote 设为 true，再通过启动参数从标准输入设置凭据。Linux 示例：

  read -r -s -p 'GM password: ' QQTANG_GM_PASSWORD; echo
  printf '%s\n' "$QQTANG_GM_PASSWORD" | ./start-server-linux-amd64.sh -gm-username admin -gm-password-stdin
  unset QQTANG_GM_PASSWORD

  ARM64 替换脚本名即可。Windows PowerShell 示例：

  $credential = Get-Credential -UserName admin
  $credential.GetNetworkCredential().Password | .\start-server-windows.cmd -gm-username $credential.UserName -gm-password-stdin

  两个参数必须成对使用；服务端只保存 PBKDF2 加盐校验值。以后凭据不变时可直接运行启动脚本，不必重复传入。

存档与故障排查

- 首次启动会自动创建 runtime/data/qqtang.sqlite。
- 默认账号背包包含 1 张单人探险卡和 500 瓶大体力药水，不预装时装。
- 图形启动器日志：runtime/logs/launcher-ui.log。
- 服务端日志：runtime/logs/server-local.jsonl、local-server.stdout.log 和 local-server.stderr.log。
- 客户端日志：runtime/logs/local-client-launch*.json、local-launcher*.stderr.log。
- 不要关闭 Windows Defender、防火墙，也不要从第三方 DLL 网站下载系统文件。
