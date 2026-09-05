# 物品 ID 目录生成说明

`item-id-catalog.json` 和 `item-id-catalog.csv` 不是手工维护表。它们由 Go 命令合并当前完整客户端的 `object/itemCFG.py`（完整账号物品注册表）、`config/propdescrip.xml`（版本 108）和 `config/DealProp2.xml`（版本 110）。三份文件均按 GBK/GB2312/CP936 解码；重复 ID 以较新的 DealProp2 名称和描述为准，同时保留全部分类与来源文件。

重新生成：

```powershell
go run ./cmd/qqt-item-catalog -client-root runtime/client-patched -json docs/item-id-catalog.json -csv docs/item-id-catalog.csv
```

字段含义：

- `id`、`name`、`categories`、`description` 来自客户端配置，置信度为 `confirmed-client-config`。
- `kind` 是根据客户端分类生成的稳定英文类别，供 Go 配置和 SQLite 使用。
- `client_scene_factory_supported` 只表示当前 Client.exe 的场景对象工厂存在该 ID 的构造路径；它不证明某张地图会预置该物品，也不证明某只怪物会掉落该物品。
- 背包消耗品（例如 20044 小体力药水）、开局场景元素和怪物击杀掉落是三种不同用途。后两者必须继续结合地图配置、怪物掉落表和实际事件确认。

当前生成结果共 3123 个唯一 ID。它已覆盖原先商店 XML 没有列全的背景、装扮、宠物粮食和宠物卡：1=秋天的气息、21=八路军帽、26002=宠物粮食小、28001=普通的酷比、30044=冰沫。示例中的“场景工厂支持”仍只代表客户端能创建该类地图对象，不等于官方掉落概率证据。
