# Python 字节码构建依赖

本目录是随源码提供的第三方 Python 库，供
`scripts/patch-python23-item-registry.py` 处理原始客户端的 Python 2.3 字节码，
写入单人 BOSS 卡、AI 对战卡的道具名称和说明。它不是 Python 解释器，也不是 AI 训练环境。
构建机器仍需安装 Python；游戏运行和 ONNX 推理不使用本目录，发行包不携带这些库。

| 随库版本 | 用途 |
| --- | --- |
| xdis 6.1.7 | 读取旧版字节码、指令和代码对象 |
| Click 8.4.2 | xdis 声明的命令行依赖 |
| six 1.17.0 | xdis 声明的兼容依赖 |
| Colorama 0.4.6 | Click 在 Windows 上的依赖 |

这些库和各自的 `.dist-info` 元数据、许可文本一起进入 Git，使字节码补丁步骤无需临时
联网安装 Python 包。建议使用 Python 3.10 或更新版本，符合随库 Click 的版本要求；
第三方许可见仓库根目录 `THIRD_PARTY_NOTICES.md`。

更新依赖时只复制表中的库及其元数据、许可文本，不要复制整个 Python 包安装目录。
`uncompyle6`、`spark_parser` 及其 `.dist-info` 是反编译分析工具，不属于发行构建依赖。
顶层 `bin/` 中的安装器启动脚本、`__pycache__/` 和 `.pyc` 缓存也不需要复制或提交。
构建直接调用项目脚本，不依赖这些命令行入口。
