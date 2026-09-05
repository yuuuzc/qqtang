# Runtime 维护

`runtime` 同时保存可再生成构建物和不可替代的动态证据，不能整目录清空。

默认预览：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1
```

确认清单后删除默认安全目标：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -Apply
```

默认目标包括全部可重建 Go cache（含历史 `go-cache-codex` 和模块下载缓存）、历史版本 EXE/`.next.exe`、旧验证构建、已完成的补丁 QA/启动冒烟目录、`inspect-*` 输出、`runtime/tmp`、预览图和已归一化的 tracer 输出；固定名当前程序、两个客户端副本、抓包、SQLite、资源缓存、补丁、包样本、Ghidra/IDA 工程及普通证据日志均受保护。

开发阶段产生的 `runtime/build`、`build-check`、`inspect-*`、`tmp`、`tmp-preview` 和未被正式材料引用的旧 tracer 属于可再生成工作区。它们应在结论写入 `docs/`/`evidence/`、来源版本固定后删除；不得让临时目录成为唯一证据来源。

显式清理旧日志：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeLogs
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeLogs -Apply
```

该选项保留文档、证据、脚本、配置或 Go 源码中按文件名引用的所有日志，同时保留最近 24 小时写入的文件；`-LogRetentionHours` 可调整保留窗口。只有早于截止时间且未引用的逆向分析输出会进入删除清单。

显式清理旧的 `runtime/analysis` 中间产物：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeAnalysisArtifacts
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeAnalysisArtifacts -Apply
```

该选项与日志清理使用同一套引用白名单、活动会话保护和时间窗口，只选择早于截止时间、没有被 `docs/`、`evidence/`、脚本、配置或 Go 源码引用的分析文件。它不会清理 `runtime/ghidra`、`runtime/ida`、`analysis` 内嵌的 Ghidra/IDA 工程、内存/二进制输入、图片和视频原件、客户端、SQLite、抓包或资源数据。可以与 `-IncludeLogs` 组合，但必须先预览；删除不进入回收站，只适用于可由现有工具和输入重新生成的中间产物。

显式清理旧抓包会话：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeCaptures
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\clean-runtime.ps1 -IncludeCaptures -Apply
```

该选项按完整会话目录处理，只选择早于保留窗口且未被正式材料引用的会话。当前运行和最近写入的抓包不会进入候选清单。分析、日志与抓包选项可以组合；始终先预览再加 `-Apply`。

`-IncludeAnalysisDatabases` 额外选择 `runtime/ida` 和日志目录中的 IDA 数据库。这些文件通常可由保存的内存映像重建，但不进回收站，只有明确不再需要交互式 IDA 状态时才使用。
