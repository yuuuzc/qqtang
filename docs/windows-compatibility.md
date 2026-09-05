# Windows 兼容性基线

## 主机与静态前提

- 宿主机：Windows 11。
- 主程序：`Client.exe` 5.2.1.201，x86/PE32 GUI 原生程序。
- 42 个 PE 全部是 x86；299 条静态导入在客户端目录或当前系统目录均能解析。
- 旧图形依赖：Direct3D 8 (`d3d8.dll`)；备用模块 DirectDraw (`DDRAW.dll`)。当前系统均存在。
- 未发现静态 D3DX、XInput、XAudio 依赖。
- 首次基线前未设置 AppCompat Layers；未一次同时开启多项系统兼容选项。
- 所有运行只使用 `runtime/client-patched`；当前启动链不新增客户端出站规则，也不安装旧驱动。

## 单变量系统兼容测试

每次只设置一项并在测试后清除。原始结果见 `evidence/windows-compatibility-runs.json`。

| 配置 | 实现 | 原始结果 | 结论 |
|---|---|---|---|
| Windows 11 默认 | 无 Layers | 77 ms，退出 `0xC0000005` | 基线 |
| Windows 7 兼容模式 | `~ WIN7RTM` | 约 88 ms，同一异常 | 无改善 |
| 禁用全屏优化 | `~ DISABLEDXMAXIMIZEDWINDOWEDMODE` | 约 83 ms，同一异常 | 无改善 |
| 高 DPI 由应用处理 | `~ HIGHDPIAWARE` | 约 71 ms，同一异常 | 无改善 |
| 管理员权限 | 单独 `RunAs` | UAC 后同一异常 | 无改善；默认不需要管理员 |

未关闭 Windows 安全功能，未从第三方 DLL 网站取文件，未连接官方服务。

## 原始异常与后续修正

原始异常位于 `ClientBase.dll+0x7647FF`，读取 `0x210200F0`。该位置属于受保护 `.tvm0` 运行时代码，并非 DirectX、DPI 或缺 DLL。

早期诊断曾在创建的挂起进程内固定保留 `0x21020000` 页以越过该访问；这只是定位后续链路的实验手段，最终正式实现已经删除该默认假设。继续动态分析得到：

1. `AdjustTokenPrivileges` 返回 TRUE，但旧 TP 把 `ERROR_NOT_ALL_ASSIGNED` 当失败；只在成功返回后清除该错误可继续。
2. TP 尝试服务管理器和 `\\.\TesSafe`；包内会运行时释放旧 x64 `TesSafe.sys`。该驱动从未在 Win11 宿主机安装或加载。
3. 用户态兼容句柄和 IOCTL 能继续执行，但 TP 把普通内核句柄交给 `FindClose`，Win11 在 `ntdll` 临界区路径崩溃。只为兼容句柄或 `ClientBase+0xAADD79` 调用者改用 `CloseHandle` 后该崩溃消失。
4. 随后 TP 把随机 ECX 当 KERNEL32 ETW provider 对象，在 `KERNEL32+0x10132` 崩溃。真实入口调用者动态确认为 `ClientBase+0x3FBB8`；仅对该调用返回 ETW 注册成功后原崩溃消失。
5. 后续 `mov al,[edx]` first-chance AV 位于带 SEH 的 `IsBadReadPtr` 类探针，应由函数内部捕获。通用 VEH 会制造假冻结，真实运行不得启用该诊断项。

## TesSafe 请求与 Win11 用户态兼容边界

- 固定调用点：`ClientBase+0xAB34A6`。
- 固定 IOCTL：`0x0022C400`，同址输入/输出 1024 字节。
- 用户态兼容返回 TRUE 和 1024 字节后，客户端在 `+0xADE16` 验证长度，再在 `+0xAB5420` 解码对象。
- 回显请求可通过对象完整性校验，但缺少驱动语义响应，最终仍产生 1008。
- 精确清除上下文中的 `-100` 状态命中 10 次仍未消除警告，说明该 DWORD 不是最终判定。

因此用户态兼容层已经解决“旧驱动无法安全加载”的 Win11 崩溃边界。后续动态证据表明不必完整模拟旧驱动协议即可保留必要启动初始化并隔离最终 TP 终止分支。

2026-08-13 起曾把固定 RVA 改为系统 DLL 语义扫描，但 Win10 19045 与另一 Win11 的失败证明私有 wrapper 机器码不是可发布边界。最终实现改为随包固定的 ClientBase 边界加进程内、线程级、异常类型级门控：启动上下文修复严格校验 `ClientBase+0x7647FF` 的11字节签名，通过同线程 bootstrap 注册 VEH，并把本次实际无效页按偏移重定向到零初始化 RW shadow page；捕获 TP worker TID 后，ETW 修复只接受该线程在 KERNEL32/KERNELBASE 中的一次匹配读写 AV，并要求返回链包含固定 `ClientBase+0x3FBB8` continuation。它替换无效 provider 指针后继续执行当前系统自己的 helper，不扫描、不改写也不固定 Windows 私有代码 RVA。旧 scanner/tracer 仅保留为显式诊断，权威闭环见 `evidence/windows-generic-startup-compat-v1.md`（EVID-202）。

## 当前 Windows 10/11 运行结果

当前验证命令等价于：

```powershell
runtime\bin\qqt-launch.exe -local-compat -wait 52s
```

最终干净运行证据 `runtime/logs/local-compat-tp-bypass-clean-v63.json`：

- 运行 52 秒后仍存活，进程响应正常，25 个线程。
- TP 终止序列先在同一线程以 `ClientBase+0x6C6F02 / STATUS_PRIVILEGED_INSTRUCTION` 精确武装，随后以 `EIP=EAX+3` 执行 `BOUND` 制造 `STATUS_ARRAY_BOUNDS_EXCEEDED`；严格门控命中 1 次，只结束该 TP 工作者。
- 唯一可见顶层窗口为已启用、未挂起的 `QQTangWinClass / QQ堂 5.2 Beta1 Build1`。
- TP 1008 对话框为 0，“程序错误”错误上报器为 0；没有加载 `TesSafe.sys`。
- DXGI Desktop Duplication 实际捕获到 QQ堂 4.3 主题的初始登录面板；旧 overlay 的合成帧包含退出前 IDM 弹窗残影，因此窗口树和 PID 审计仍是“无遮挡/无错误窗口”的权威证据。

上述主机历史样本证明 Win11 基础启动链可用；后续通用方案又用同一启动器完成三环境动态验收：

| 环境 | 系统入口（仅记录，不作为定位条件） | 结果 |
|---|---|---|
| 开发机 Windows 11 `10.0.26200` | `KERNEL32+0x10132` | 10秒等待后仍运行；shadow-context 68次修复；ETW stage 8完成 |
| VirtualBox Windows 11 `10.0.26200` | `KERNEL32+0x10132` | 10秒等待后仍运行；同上 |
| VirtualBox Windows 10 `10.0.19045` | `KERNEL32+0x54245` | 10秒等待后仍运行；shadow-context 68次修复；ETW stage 8完成 |

三份结果的 `reserved_pages` 均为空，启动器 SHA-256 均为 `8EA5536BBD69961BA1A95687682F041E14462FD7DBAD0756147FC2971920EA22`。Win10 与 Win11 实际入口 RVA 不同而同一实现均通过，直接证明正式路径不依赖某个系统 build 的私有 RVA。最终 `QQTang-Local.zip` 包含相同启动器，静态检查通过，用户又对完整 Release 实机确认“都通过”。因此 BLOCK-015 已解决；支持边界是已验证的 Windows 10/11 x64，而不是对任意未来 Windows build 的无条件保证。

2026-08-04 的后续默认环境验证进一步到达真实服务器列表：客户端使用虚拟本地账号连接 `127.0.0.1:18080`，发送 202 字节目录请求；Go 服务端返回经客户端自身 `QQTEncoder` 生成的 186 字节 `RESPONSE_HALL_INFO`，客户端解码成功、发送 82 字节后续消息，并由未插桩的原生 UI 显示 `Local / Local Section / 快>>>`。

2026-08-05 的继续验证进一步越过游戏服登录：客户端向 `127.0.0.1:18000` 发送 210 字节、运输命令 `0x0064` 的登录请求；Go 服务端返回 202 字节、同一运输命令 `0x0064`、schema `0x07D5` 的登录成功响应。客户端接受响应，加载原生新手教程，用户可正常操作并实际通关，随后进入原生游戏大厅；玩家列表显示 `LocalPlayer`，区服路径显示 `Local >> Local Section`。截图为 `evidence/client-local-lobby-20260805.png`。

上述全过程没有设置系统 AppCompat Layers、管理员权限或 Win7 模式，没有关闭 Windows 安全功能，也没有安装/加载旧 `TesSafe.sys`。这证明 Windows 11 默认环境已满足启动、登录 UI、目录、游戏登录、新手教程和大厅运行条件。

## 启动配置矩阵（进程内诊断）

| 配置 | 结果 |
|---|---|
| 仅预留 `0x21020000` | 越过首个 AV，出现 TP 540 错误链 |
| 加 AdjustToken 错误修正 | 越过首个权限误判，进入服务/设备链 |
| 加 TesSafe 用户态句柄/IOCTL | 完成设备调用；暴露错误 FindClose |
| 加窄范围 FindClose 转换 | 原 `ntdll+0x541D0` 崩溃消失 |
| 加 `ClientBase+0x3FBB8` ETW 精确绕过 | 原 `KERNEL32+0x10132` 崩溃消失 |
| 早期兼容集合（历史） | 创建游戏窗口，最终停在 1008 警告 |
| 再加通用 AV VEH | 会误拦截预期 `IsBadReadPtr` first-chance AV；禁止用于正常启动 |
| 提前屏蔽全部 DialogBox | 改变保护线程时序并正常退出；不是可接受最终修复 |
| 堆轮询清除 1008 | 候选是 Python 元数据；已否定 |
| 三种对话框后合成返回 | 均破坏保护数据栈并产生确定 AV；禁止作为最终修复 |
| 精确清除 TesSafe context `-100` | 命中 10 次但相同 1008 仍出现；已否定 |
| 透明 IO 返回/对象 trace | 10/10 对应，未改变 1008 基线；用于恢复协议 |
| 绕过整个 ClientBase PE 入口 | TP 对话框消失，但出现 `R6030 - CRT not initialized`；必要 CRT 初始化被破坏 |
| 冻结精确 TP 对话框线程 | 60 秒进程存活，但无顶层窗口、无连接、服务端 0 包；不是可用修复 |
| 让 `ClientBase+0x70708B` 工作线程整体返回 | 1008 命中 0、线程数降至 14，但 70 秒无 UI/无连接/0 包；该线程还承担必要启动信号 |
| 最终 TP VM 操作数 `0x76 -> 0xF5` | 代数上把最终错误类 3 解码为 0；保留必要启动，原 1008 对话框消失并创建登录窗口 |
| 仅做上述错误类改道 | TP 改为利用显卡驱动数据中的 `BOUND` gadget 故意触发未处理 `0xC000008C`，旧 `QQTHelp.dll` 弹“程序错误” |
| 错误类改道 + 严格终止异常门控 | 同线程须先命中 `ClientBase+0x6C6F02 / 0xC0000096`，再满足 `0xC000008C、EIP=EAX+3、opcode=0x62`；仅结束该 TP 工作者，登录窗口持续可用 |
| 历史 Win11 `-local-compat` 收口 | 已包含上述两项；52 秒干净复跑中唯一可见窗口为 QQ堂主窗口，无 TP/错误上报器 |
| `-local-compat` + 本地登录回调 + Go 目录响应 | 客户端成功解码 schema `0x0816` 并显示真实区域/服务器列表；Win11 无新增兼容阻塞 |
| 上述链路 + Go 游戏登录 `0x0064/0x07D5` 响应 | 客户端实际完成新手教程并进入游戏大厅；Win11 无新增兼容阻塞 |
| 通用 shadow-context + gated ETW provider repair | Win10 19045、Win11 26200 VM、Win11 26200 主机使用同一二进制全部通过；正式路径无固定 reserved page/系统 RVA |
| 最终完整 Release | manifest/静态兼容检查通过；用户实机确认全部通过 |

## 事件、异常和栈证据

- 初始调试：`evidence/client-startup-debug-summary.json`、`runtime/logs/client-debug-v3.jsonl`。
- FindClose：`docs/reverse-engineering/ida-kernelbase-tessafe-av-v1.txt`。
- ETW：`docs/reverse-engineering/ida-kernel32-tessafe-post-findclose-av-v2.txt`、`runtime/logs/tp-clean-final-error-v1.json`。
- 1008 对话框：`runtime/logs/tp-warning-1008-p36612.json`。
- 1008 栈页：`runtime/logs/tp-warning-1008-stack-p14268-t34148.bin`。
- 1008 堆对象：`runtime/logs/tp-warning-1008-fullscan-p14268.json`。

## Win7 边界

Win10 19045 与 Win11 26200 已覆盖当前发布支持基线，Win7 不是运行目标或前置条件。现有 VirtualBox 的 Win10/Win11 虚拟机已经完成通用启动方案动态验收，不需要再用 Win7 或兼容模式替代证据。
