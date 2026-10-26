# 在线更新实现与发布约定

客户端默认清单：

<https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json>

只有真实上传、匿名下载及哈希核对成功后，Actions 才将此清单推进到新版本。不会提交示例更新清单。GitHub 清单和发行资产的国内可达性尚未从用户网络验收；可在 GUI 配置经过独立部署和验证的 HTTPS 清单地址。

清单字段为 schema_version=1、version（严格三段版本，可含 prerelease）、platform=windows-amd64、download_url、sha256、size、notes、source_commit、windows_game_tested。没有有效哈希、大小或正确平台时拒绝安装。SHA-256 用于内容完整性检查，不是代码签名。

流程：检查版本→用户确认→新建唯一事务目录→HTTPS 下载→大小/SHA-256/PE 校验→准备计划→复制当前 EXE 为独立更新助手→退出主程序→等待父进程退出→再次校验新旧文件→将当前 EXE 改名为唯一备份→装入新 EXE→启动 GUI→等待版本/随机令牌/二进制哈希回执→提交。

新 EXE 必须成功创建主窗口并运行消息循环，才能发送健康回执。此检查不代表游戏识别通过。启动失败或25秒没有有效回执，先停止本次新进程，再把失败 EXE 保留在事务目录，从已核实的备份复制恢复旧版并重新启动。若停止进程、移动文件或恢复失败，结果为 recovery_required，备份保持原位，不谎报回滚成功。

每次备份名含事务 ID；拒绝覆盖同名备份。历史备份和失败二进制不会自动删除。事务 result.json 记录 prepared、backed_up、awaiting_health、committed、rolled_back、unchanged 或 recovery_required。丢电中断不是普通异常：若目标文件缺失，恢复入口只能补回缺失目标，不能覆盖现有程序。异常事务可通过主界面查看。

配置、标定、单机验证资料在 %LOCALAPPDATA%/MHXStudioNext，与旧版 %APPDATA%/MHXStudioV3 分开。更新助手只操作本次目标 EXE、事务及其备份，不替换配置目录。GUI 保存设置时保留未知顶层字段。

发布工作流使用 Go 1.26.10；Linux go vet/竞态测试；Windows go vet/单元测试；真正运行 GUI、替换/回滚/篡改/文件占用用例。发布总是创建新版本和新资产，不使用 clobber。先上传候选发行版并匿名验证，再从已发布的 preview.1 实际点击更新按钮升级至候选版，确认重启、备份和配置不变后才推进默认清单；之后再匿名下载默认清单与 EXE 复核，并追加发布验证报告。匿名校验失败时保留为 draft，清单不推进。

稳定版尚未宣称验收完成；本次是 preview 渠道。版本号由 Actions run_number 生成，客户端可比较 preview.9 与 preview.10。主程序不依赖 Python、PowerShell、浏览器组件或额外安装运行库；仓库历史辅助脚本原样保留。
