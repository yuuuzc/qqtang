# QQ堂本地怀旧版使用说明

## 开始游玩

1. 完整解压整个 `QQTang-Local` 文件夹，不要直接在压缩包中运行，也不要覆盖到旧版本目录。
2. 双击 `QQTang-Local` 内的 `start.cmd`。
3. 首次启动会出现一次管理员确认，仅用于给本目录的 `Client.exe` 创建出站隔离规则；游戏本身仍以普通权限运行。
4. 使用本地账号 `1000001` 登录。
5. 结束游玩后双击 `stop.cmd`，等待服务端和客户端全部退出。

## 存档

首次启动会自动创建本地 SQLite 存档，并初始化默认人物、500 张单人探险卡和 500 瓶大体力药水。存档位于：

`QQTang-Local\runtime\data\qqtang.sqlite`

更新版本时请先备份这个文件，并把新版本解压到一个全新目录。

## 环境要求

- Windows 11
- 无需安装 Go
- 无需安装 PowerShell 7，系统自带 PowerShell 5.1 即可
- 不需要关闭 Windows Defender 或其他 Windows 安全功能
- 不要使用真实 QQ 账号或密码
