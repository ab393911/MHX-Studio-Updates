# MHX Studio

当前流程、用户最新修订与尚未接通的执行器见[开发状态与规则逐条对照](docs/CURRENT_FLOW.md)。

当前独立开发分支基于preview.21，按2026-10-29删减包停用9项功能，保留历史任务与全部财物记录。本轮未发布，在线通道未变。见[迁移设计、测试与风险](docs/SCOPE_REMOVAL_20261029.md)。

基于用户实机验收的preview.19继续开发。后续升级只点击软件内“一键检查更新”，自动下载、SHA-256、备份、替换、重启与健康检查，保留配置、职业、包名绑定、标定及新进度/订单库。

本批提供截图候选与单台登录增强、自选任意五台启动和已绑定游戏启动、持久化小组/区服/批次计划、本地仅记账订单与额度核对。海嘟嘟仅战术预检；游戏任务执行、交易行买卖和真实发货未开放。职业候选不确定时不覆盖人工绑定，角色身份不明不报告完全成功。

- [五台同槽登录准备：实现、保护和验收边界](docs/GROUP_LOGIN_ACCEPTANCE.md)
- [交接实现/验收矩阵与具体未决问题](docs/HANDOFF_ACCEPTANCE_20261029.md)
- [183张截图离线回放报告](docs/HANDOFF_EVIDENCE_REPLAY.json)
- [长期交付规则](docs/DELIVERY_RULES.md)
- [测试说明](docs/TESTING.md)
- [在线更新机制](docs/ONLINE_UPDATE_ACCEPTANCE.md)

[默认更新清单](https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json) · [发布与测试报告](https://github.com/ab393911/MHX-Studio-Updates/releases)

源码完成、离线/自动测试、Windows界面测试、真实雷电实机分别记录。真实故障回滚仍未验收。
