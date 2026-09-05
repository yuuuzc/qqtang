# 网络地址与端口审计

## Confirmed：配置中存在的值

以下“确认”只表示字节和配置键已确认；在客户端尚未越过启动崩溃前，不能声称某项已经被实际连接。

| 文件 | 文件偏移 | 原值 | 配置端口 | 工作副本重定向 |
|---|---:|---|---|---|
| `DirCfg.ini` | 158 等 | 101.227.130.250 / 101.227.130.249 | 80、8000、443 | 127.0.0.1:18080 / 18000 / 18443 |
| `p2psvrInfo.ini` | 18、54、92 | 218.85.138.28 / 202.104.193.7 | TCP 443；UDP/STUN 8000 | 127.0.0.1:18443 / 18000 |
| `caserver.ini` | 13、31、62 | vnetca.qq.com / 219.133.38.254 / 192.168.1.250 | 80、7000 | localhost / 127.0.0.1:18080 / 17000 |
| `webserver.ini` | 13、35、65 | VnetWebPro.qq.com / 61.152.88.210 / 172.30.14.128 | 80、8080 | localhost / 127.0.0.1:18080 |
| `updatedata.dat` | 96 | http://10.1.44.50:8000/patch/ | 8000 | 未修改；更新器禁止运行 |

配置修改只发生在 `runtime/client-patched`。原始字节、修改前后 SHA-256 和 Base64 备份位于 `runtime/patches/config-redirection.json`。

## Confirmed：网络 API 导入

- `ClientBase.dll`：`connect`、`send`、`recv`、`select`、`socket`、`ioctlsocket`、`gethostbyname`（WS2_32 ordinal 已按稳定导出名解析）。
- `NetCenter.dll`：WSOCK32/WS2_32，含 `gethostbyname`、`ioctlsocket`。
- `TenQQt.dll`、`TenSLX.dll`：`connect`、`send`、`recv`、`select`、`socket` 等。
- `SSOCommon.dll`：上述 Winsock API，以及 `InternetConnectW`、`HttpSendRequestW`。
- `vqqsdl.dll`：Winsock，以及 `InternetConnectA`、`HttpSendRequestA`。

完整逐 PE 清单见 `evidence/pe-inventory.json` 和 `docs/client-inventory.md`。

## Probable：二进制中的服务地址

| 文件 | 文件偏移 | 字符串 | 判断 |
|---|---:|---|---|
| `Client.exe` | 650540 | http://qqtang.qq.com | 官网/内嵌 Web 候选，不等于游戏网关 |
| `Client.exe` | 682780 | http://%s:%d/SimpleLogon11.jsp?... | VNet Web 登录候选 |
| `ClientBase.dll` | 1380864 | http://down.qq.com/iedsafe/Client/tenprotect | 保护组件下载候选，禁止外连 |
| `ClientBase.dll` | 1383808 | http://down.qq.com/iedsafe/Client/vsandbox | 保护组件下载候选，禁止外连 |
| `ClientBase.dll` | 1401780 / 1401796 | 183.60.58.99 / tqos.gamesafe.qq.com | QoS/安全组件候选，禁止外连 |

## 当前动态配置与隔离

- `configs/network.json` schema 3 将服务端监听和客户端目标分离：服务端模式只有 `local`、`lan-host`、`remote-host`，客户端目标独立存放在 `client_server_ip`。
- Go 启动时让 `lan-host` 与 `remote-host` 都监听 `0.0.0.0`，目录响应分别公布选定的私有 IPv4 或公网 IPv4；`local` 只监听并公布 `127.0.0.1`。
- 图形启动器和 `network.cmd` 同步修改工作副本的 `DirCfg.ini`、`p2psvrInfo.ini`、`caserver.ini`、`webserver.ini`，不改 `source/QQtang`。
- 当前启动链不安装 Client.exe 出站或服务端入站规则；schema 3 不包含旧的出站隔离字段。
- GM 回环访问不加载认证；只有显式开放非回环 GM 时才强制加载有效 PBKDF2 密码配置。细节见 `docs/network-deployment.md`。
- PowerShell 5.1 已实际完成 13 个地址键的 `127.0.0.1 → 192.168.254.77 → 127.0.0.1` 可逆测试。

## 仍未知

- UDP 18000 已确认承载 QQTPPP Type 1/2/3/5，且 Type 2 已完成房间移动/糖泡双向实机验收；仍未知的是正式对局中它与 `0x10EB/0x10E7`、TCP 可靠补发的配对边界。
- `p2psvrInfo.ini` 和 P2P TCP/UDP/STUN 字符串已静态定位到 `CP2PDownloader` 连续日志簇，属于旧资源下载器；不能用来证明玩家地址互换、玩家间套接字或 NAT 打洞。`P2P_SALE_ITEM` 是玩家间物品交易名，同样不作为传输证据。
- 第二台真实 Windows 电脑上的 LAN/公网可达性、云安全组和 Windows 入站策略表现；不得用关闭全部安全功能代替诊断。

## 对局联网结论

- 当前恢复服务选择中心服模型：所有客户端连接同一个目录/大厅/对局服，房间事件由 Go 服务端验证并广播。这是面向 LAN/公网的恢复架构，不是对原腾讯拓扑的反向断言。
- 原版客户端生成的 UDP Type 2 同样由 Go 中心服按 `PlayerID+UIN` 和已认证 Type 1 presence 定向中继；基础实时同步不依赖 peer state 进入直连状态 5。
- 同一局域网内不跨 NAT，因此不存在“打洞”前置条件；客户端只需把独立的 `client_server_ip` 指向主机公布的私网 IPv4。
- 互联网模式也不做玩家之间 NAT 打洞：客户端均向公网中心服发起出站连接。服务端若在家庭 NAT 后，需手动端口转发或可信 VPN；云主机则配置入站安全组。当前 Go 服务端不实现 STUN/ICE/UPnP 协调服务。
