# 当前已交付开发实测版

记录时间：2026-11-01，上海时间。

- 当前默认在线更新通道：3.5.0-preview.40。用户现有preview.21可以在软件内点“一键检查更新”直接升级；禁止要求手工下载/替换EXE。
- 实际发布源码：bfc5e2f0336ede93e963d364cac21fb1a2f15a43。
- EXE SHA-256：e58eaef41b4e37361f79cc500872bc89f1e7ebe37d810785740b294eb397daa2；大小8266240字节。
- 更新清单：https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json
- 发布资产清单：https://github.com/ab393911/MHX-Studio-Updates/releases/download/v3.5.0-preview.40/update.json
- 实际下载：https://github.com/ab393911/MHX-Studio-Updates/releases/download/v3.5.0-preview.40/MHXStudio.exe
- Actions：https://github.com/ab393911/MHX-Studio-Updates/actions/runs/41301129435

已核对：Windows基础GUI、EXE构建、私有HTTPS preview.19/.21直升、配置/SQLite保留、更新失败恢复、篡改/占用拒绝；真实公开候选链接的preview.19/.21一键升级及新更新器一键升级均通过。发布与默认清单推进成功，随后再次匿名下载默认清单对应EXE核对SHA/Windows x64架构。

这不是全功能验收：中文货币原始截图OCR被分到单独实验性检查，不纳入基础通过结论；记录时仍在运行，不能说已通过。所有新游戏模块未通过用户真实雷电/游戏验收；真实电脑故障回滚也仍未验收。

本版测试入口：工作台→五台同槽登录准备，选择组队/八星链，或资产、聚福、仙盟战令/打工簿、本周赛季首领识别单项。取消昵称核对/标定以及订单、发货、消息入口。保存单项为完整自动流程已明确拒绝，避免误消费。

尚未交付：据点实战、交易行完整买卖、福气不足自动兑换、全角色/区服/批次轮转。`codex/mhx-market-execution-20261101`中的报价读取/导航代码及其测试是后续开发，不包含在preview.40，不能告诉用户本版有该入口。对应源码已经提交并继承当前交付与启动模式修正，继续从这里开发。

本文件覆盖此前文档中“尚未发布/当前清单仍21”的旧状态。用户已授权持续开发并要求先交付机器测试版本；不再逐模块索要继续授权。测试步骤见TEST_THIS_BUILD.md。
