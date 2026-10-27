# MHX Studio 接手盘点

盘点时间：2026-10-27（北京时间）。只记录已读取到的内容。

## 接手时的 GitHub 仓库

仓库：<https://github.com/ab393911/MHX-Studio-Updates>

原始 main 提交：`efc61ddb717d1e182d95547a43a29dfd31fe0db1`。

仅有 README.md、UPDATE_PROTOCOL.md、五个 PowerShell 辅助脚本和一个 Python ADB 绑定工具。只有 main 分支，没有 Git 标签、Actions 工作流或 Releases。未发现 AGENTS.md。未包含主程序源码、图像识别模板或游戏状态机。

原 UPDATE_PROTOCOL.md 明确注明在线更新未发布。原 Update-MHXStudio.ps1 是独立脚本，并未集成主程序。原 V3 updater.go 存在主动移除旧备份、缺少新版本健康检查等问题，本次不复用其替换逻辑。

## 可获取的历史资料

| 文件 | SHA-256 | 核实结果 |
| --- | --- | --- |
| MHX_Studio_V3_3_Test.zip | 60690f1cc33e845227625ebe191175b12e24b5e9fe2830cb00bda799ca43569c | 包内只有 7,956,480 字节的 V3.3 EXE 和说明，没有 V3.3 源码。 |
| MHX_Studio_V3_4_Alpha.zip | 36547ecccd98262f3e6ea9eb05cd0430afd68bf915080072a46a75c1733f62b7 | 有 EXE、5,099 字节 main.go 和说明。实现 ADB、人工逐步确认点击、小范围随机点击与延迟；没有自动页面识别、GUI、在线更新、五开闭环或换区。 |
| MHX_Studio_V3_Source.zip | 76ca75ee6e26b257422a487861f1f5bad8fc8119625358e0573594cf60cb5a12 | main.go、updater.go、说明和示例清单。是 V3.0 记录器源码，不能冒称 V3.3 源码。 |

本次保留原仓库所有文件，不将历史二进制、游戏账号资料、截图、配置或凭据推到公开仓库。

## 本次新增源码的边界

- cmd/mhxstudio：原生 Windows GUI；独立数据目录；主程序一键更新；识别标定、单机/五开登录测试入口。
- internal/update：HTTPS 清单、严格版本比较、大小与 SHA-256 校验、Windows x64 PE 检查、唯一备份、独立更新进程、GUI 启动回执、失败回滚和事务记录。
- internal/ldplayer：list2动态实例、窗口句柄/进程信息、Android ID加boot ID核对ADB绑定；新增独立ADB和本机ADB智能套接字发现。无法证明编号时只显示设备，不猜端口对应编号。
- cmd/mhxstudio/discovery_store.go：自动保存已经核验的发现路径，原config.json保持原位；检测详情报告列出检查路径与错误。
- internal/automation：两处图像特征、角色选中和角色昵称核验、按钮范围内微小随机点击、可配置重试、有限内存截图、任务完成换区闸门。
- .github/workflows/windows.yml：Linux 测试、Windows 原生测试、EXE 构建、追加发布版本、匿名下载复核、独立 updates 分支清单。

职业目前为人工绑定；没有自动职业 OCR。没有战斗、自动任务完成证明、银锭交付、自动启动下一组或实际换区动作。换区闸门默认关闭，跳过任务不会算完成。登录测试不写任务完成状态。

源码编译与自动测试、Windows GUI/文件操作测试、真实雷电/游戏实机验收是三个不同层级。发布资产内 WINDOWS_AUTOMATED_TESTS.json 只证明自动测试，`windows_game_tested` 始终为 false，直到实际游戏验证后另行修改。
