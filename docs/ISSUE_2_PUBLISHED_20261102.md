# Issue2 在线候选测试通道已发布

2026-11-02（上海时间）。用户明确接受 preview.40 首次升级助手被强制中断后可能需要人工 GUI 恢复的剩余风险；没有把该限制改称无人干预恢复。此前的严格发布阻止记录保留为历史。

## 当前交付

- 版本：`3.5.0-preview.41-issue2.1`，仅 Issue2 预发布候选。
- 运行 EXE 的真实源码：`f3bc322408aaa92fcc91d8fac0133144238d8184`。
- 发布控制器/恢复文档提交：`0e9641abdb72ba3afac236595fa511cf3ee809e2`。
- Windows x64 EXE：8,340,992 字节；直接复用原生 Windows 已验收产物，不重新构建业务 EXE。
- SHA-256：`b7ac42b0800b96c4dd60a4245e31ebf054373675ae23702af4bb7ba978bf3cb7`。
- 独立测试清单：https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/issue2-test.json
- 清单所指发布资产：https://github.com/ab393911/MHX-Studio-Updates/releases/download/issue2-v3.5.0-preview.41-issue2.1/MHXStudio.exe
- 发布清单：https://github.com/ab393911/MHX-Studio-Updates/releases/download/issue2-v3.5.0-preview.41-issue2.1/update.json
- 仅预发布的版本页面：https://github.com/ab393911/MHX-Studio-Updates/releases/tag/issue2-v3.5.0-preview.41-issue2.1

测试标签采用 `issue2-v...`，不匹配原有 `v*` 自动发布入口。发布只写 `updates/issue2-test.json`，没有修改默认 `preview.json`；默认文件在操作前后的 Git blob SHA 均为 `fc0cb8c83a5c3aad7df831354b930fe82c4fb013`，另在代理环境独立取回并逐字节比较相同，仍为 preview.40。

## 构建、回归和公网验证

- 历史原生构建与更新验收：https://github.com/ab393911/MHX-Studio-Updates/actions/runs/41438510528 。Linux 竞态 254 项、Windows 单元 256 项通过，原版40正常一键升级、配置/SQLite/绑定/角色历史、损坏文件拒绝和启动失败回滚通过。
- 本次发布工作流：https://github.com/ab393911/MHX-Studio-Updates/actions/runs/41450117973 。所有发布、Windows公网验证、测试清单提升与证据作业成功。
- 发布前比较最新工作树与实测源码的 `cmd`、`internal`、`go.mod`、`go.sum`：无差异；不恢复已删除功能。
- 对下载的历史原生 EXE 再核对完整 SHA/大小/元数据。草稿资产通过网络下载并核验哈希后才公开为预发布候选。
- 匿名从实际发布清单下载真实 EXE，SHA-256、Windows x64 PE、版本与源码字段核对通过。
- 在一次性 Windows VM 运行真正发布的 preview.40，界面保存测试清单地址、一次点击更新，公网资产下载/校验/备份/替换/可见GUI重启通过；配置和标定原字节、SQLite原订单、角色执行记录、五台绑定及历史备份保留。
- 新增 GUI 人工恢复测试：模拟已验证可达的“原 EXE 已备份、目标缺失”持久状态，直接启动原版 updater.exe **不带参数**，点击恢复窗口“是”；旧 EXE、配置原字节、SQLite 订单和可见旧 GUI 恢复通过。没有把测试程序调用命令行当作用户恢复步骤。
- 提升独立测试清单后，再匿名下载与哈希验证；代理云环境另一次独立下载也通过，字节与原生 Windows 产物一致。

原始发布与 GUI 报告随预发布资产附带：`ISSUE2-VERIFICATION.json`、`published-gui-update.json`、`manual-recovery.json`、`test-channel-download.json`、`issue2-publication.json`。

测试只使用临时配置、SQLite、订单哨兵和模拟器接口替身；没有运行真实游戏任务，真实雷电尚未验收。

## 用户操作

可以从 preview.40 软件内升级。首次将“更新清单”填写为本测试清单 HTTPS 地址，点“保存设置”，再点“一键检查更新”。以后直接检查即可。保持默认清单不变意味着未加入测试通道的 preview.40 不会自动收到此候选。

无需手动下载ZIP/EXE、复制替换文件、CMD/BAT或Python。不直接操作用户电脑，不清配置和数据库。原有自动计划保留，本次请在软件中暂停业务自动执行，仅按 Issue2 分阶段验收。

## 强制中断恢复

完整恢复步骤见 [ISSUE_2_TEST_CHANNEL_RECOVERY.md](ISSUE_2_TEST_CHANNEL_RECOVERY.md)，同内容随发行附件 `ISSUE2-RECOVERY.md` 提供。

先记下原安装目录。若本次强制中断导致主 EXE 缺失，在资源管理器进入该目录 `.mhx-updates\tx-...`，核对本次事务，双击 `updater.exe`，在“MHX Studio 恢复”窗口点“是”。不手工复制 EXE。备份在 `.mhx-backups\tx-...-原文件名.exe`，配置/SQLite快照在 `.mhx-backups\tx-...-config`；确切路径记录于 plan.json。

目标仍存在、校验失败或恢复无效时停止操作并保留所有文件，不能删除目标来强行恢复。自定义数据目录的恢复后启动场景本轮未测试；若自动重启到了默认目录，用原有启动快捷方式重新打开，不新建或复制数据库。原版快照容量限制和极端磁盘故障仍需单独处理。
