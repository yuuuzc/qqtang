# 本机、局域网与公网部署

## 启动入口与配置模型

发布包完整解压后使用根目录 `QQTang-Launcher.exe`。它是 32 位 Windows GUI 程序，通过 Windows 10/11 自带的 Windows PowerShell 5.1/WinForms 在隐藏后台执行脚本，不需要 Go、Node.js 或 PowerShell 7，也不显示控制台窗口。

`configs/network.json` schema 3 将服务端和客户端配置分开：

- `mode`：服务端监听模式，仅允许 `local`、`lan-host`、`remote-host`。
- `server_ip`：服务端在目录响应中公布给客户端的 IPv4。
- `client_server_ip`：本机客户端实际连接的 IPv4，与服务端模式互不绑定。
- `gm_remote`：是否允许非本机访问 GM。

| 服务端模式 | 实际绑定 | 公布地址 |
|---|---|---|
| `local` | `127.0.0.1` | `127.0.0.1` |
| `lan-host` | `0.0.0.0` | 选定的 RFC1918 私有 IPv4 |
| `remote-host` | `0.0.0.0` | 用户填写的公网 IPv4 |

服务端和客户端拥有独立启动/停止按钮。`start-client.ps1` 在没有本地服务端和 `run-state.json` 的情况下也能独立连接远端服务器；重复启动自动分配下一个客户端实例。`stop-clients.ps1` 只停止全部客户端及辅助进程，`stop-server.ps1` 只停止 Go 服务端。

## 端口与公网拓扑

公网服务端需要由部署者在云安全组或家庭路由器中开放 TCP 17000、18000、18001、18080、18443 与 UDP 18000。商城 Type-3 目录和 Type-2 会话共享 `18001/TCP`。玩家客户端只向中心服发起出站连接，不需要互相做端口映射；服务端位于家庭 NAT 后时，由服务器主人手动转发端口或使用可信 VPN。

局域网与公网模式都使用 Go 中心服中继 QQTPPP 实时数据。项目不实现 STUN/ICE/UPnP 打洞；旧 `p2psvrInfo.ini` 中的下载器 STUN 字符串不作为正式对局拓扑依据。

## Windows 防火墙和权限

启动链不会创建、修改或删除防火墙规则，不调用管理员提权，也不关闭 Windows 安全功能。schema 3 不包含任何防火墙控制字段。

局域网/公网服务端首次监听时，Windows 可能显示系统自带的“允许访问”窗口。主机方只应在可信的专用网络上允许 `runtime\bin\qqt-server-local.exe`。如果误点拒绝，需要在 Windows“允许应用通过防火墙”中手动恢复；在不提权、不写防火墙规则的约束下，启动器不能替用户自动放行。

## GM 访问

GM 本机地址固定显示为 `http://127.0.0.1:18100/gm/`。回环访问不加载认证配置，因此不需要账号密码。

只有显式启用 `gm_remote` 时，服务端才绑定非回环地址并强制加载 `configs/gm-auth.json`。启动器直接提供用户名和密码设置；密码使用随机盐和 PBKDF2-HMAC-SHA256 保存，不写明文。HTTP Basic 本身不加密传输，公网管理仍应通过 HTTPS 反向代理或可信 VPN。

## 诊断

GUI 的每次后台操作都会把时间、脚本、参数、退出码和完整输出追加到 `runtime/logs/launcher-ui.log`。服务端和客户端仍保留各自的细粒度日志，因此美化界面不会降低开发调试便利性。
