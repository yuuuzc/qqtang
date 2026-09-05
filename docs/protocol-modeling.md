# Go 协议类型建模约定

已由 `QQTMsgData.bin`、原生编码器、客户端消费函数或真实网络向量确认的结构，必须先定义为 Go 类型，再由该类型集中负责字节转换。业务代码不得重复声明同一布局，也不得直接依赖某个字段的裸偏移。

## 当前类型

| Go 类型 | 原始结构/用途 | 编码入口 | 解码入口 |
|---|---|---|---|
| `PlayerProfile` | 可编辑 JSON 人物档案；不是线协议对象 | `ToLocalLoginConfig` | `encoding/json` |
| `GameInfo` | `GAME_INFO`，战绩、积分、等级、角色及扩展战绩 | `AppendNetworkBinary` | `ParseGameInfoNetwork` |
| `ItemInfo` | `ITEM_INFO`，18 字节标准背包项 | `AppendNetworkBinary`、`MemoryBinary` | `ParseItemInfoNetwork`、`ParseItemInfoMemory` |
| `LoginResponse` | `0x07D5 / RESPONSE_LOGIN` 完整成功响应 | `MarshalNetworkBinary` | `ParseLoginResponseNetwork` |
| `GameItemType` | `GAME_ITEM_TYPE`，ItemID 与正数 Quantity 的六字节候选项 | `appendNetworkBinary` | 由 `ParseGameBeginDataNetwork` 解码 |
| `PlayerGameInfo` | `PLAYER_GAME_INFO`，含最多十种 NewItems | `AppendNetworkBinary` | 由 `ParseGameBeginDataNetwork` 解码 |
| `GameBeginData` | `0x041C -> 0x0FA1` 的地图开始数据，含三组候选数组 | `MarshalLengthPrefixedNetworkBinary` | `ParseLengthPrefixedGameBeginDataNetwork` |

人物 JSON 到线协议的路径是：

```text
PlayerProfile
  -> ToLocalLoginConfig()
  -> NewLocalLoginResponse(UIN, config)
  -> LoginResponse.MarshalNetworkBinary()
```

教程完成标记、等级/积分上限和永久单人探险卡默认值都在这条类型转换路径中处理。服务端业务代码只读取字段名，不写 `payload[59:63]` 或 18 字节手工数组。

## 约束

1. 结构名优先沿用客户端 schema 名；JSON 标签可以使用更易编辑的名称。
2. 网络大端和 x86 进程内小端必须使用不同的显式方法，不能靠调用方传布尔值猜测。
3. 固定大小和字段偏移只允许在对应类型文件内定义；长度由前一字段宽度累加，避免互不关联的十进制常量。
4. 变长数组必须校验数量、剩余长度和项目上限；解码完成后不得静默接受尾随数据。
5. 尚未确认的受控数组保持为空并拒绝非零数量。不能为了让包“看起来完整”而猜字段。
6. 每种布局至少保留一个精确字节向量、一次编码/解码往返和短数据拒绝测试。
7. Go 结构体可能包含对齐填充，不能以 `unsafe.Sizeof` 代替线协议长度。
8. Windows 进程内存布局只在有静态或运行时证据时加入；它与网络布局分开验证。

## 修复证据准则

协议、场景同步和客户端兼容问题必须先闭合因果链，再提交修复：

1. 客户端源码/反编译、原生生产者与消费者、协议布局和真实网络记录是定规则的主要证据；截图、手感和单次现象只用于复现、缩小范围及验证结果。
2. 修改前必须说明事件由谁产生、经什么通道和顺序传输、由谁消费、依赖哪些既有状态；无法证明的环节明确标成未知，不用猜测字段、补偿时序或白名单掩盖。
3. 修复应落在产生不一致的最早边界，并保持原生身份、通道、顺序、批次和非幂等语义；不得把“可靠送达”误当成“可跨通道独立消费”。
4. 回归测试必须锁定已证明的因果合同，同时保留反例：既验证修复后的正确路径，也验证旧的跨通道、重复执行或事后兜底不会重新出现。

新增已确认类型时，应把结构实现放在 `internal/protocol/game`（或对应协议包），把客户端内存定位留在 `internal/tooling/winlaunch`。内存工具读出完整记录后调用协议类型解析方法，不再逐字段切片。

顶层 transport command 必须集中登记在 `internal/protocol/game/commands.go`，跨消息枚举族集中在 `enums.go`。单条消息独有的 schema ID、事件 ID、长度和数组上限仍与 codec 放在一起。不能在处理器、测试夹具或不同消息文件中重复声明同一个命令号；已确认的同号请求/响应必须以显式别名表达，并由注册表冲突测试覆盖。服务端各层的完整依赖边界见 [server-architecture.md](server-architecture.md)。
