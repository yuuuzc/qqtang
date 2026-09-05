# 原始客户端放置目录

本目录不随仓库分发原始 QQ堂客户端。请自行取得与本项目兼容的原始客户端，并把客户端根目录下的全部内容直接解压到这里。

放置完成后至少应当存在：

```text
client/original/Client.exe
client/original/Core.dll
client/original/QQTDir.dll
client/original/QQTModules.dll
client/original/QQTSection.dll
client/original/config/GameCFG.ini
```

当前静态补丁针对客户端主程序版本 `5.2.1.201`。构建过程只读取本目录，并先复制到 `release/<包名>/runtime/client-patched`；不会原地修改这里的文件。补丁工具会验证它实际修改的二进制位置，客户端版本不兼容时会明确停止。

不要把解压后的客户端文件提交到 Git。
