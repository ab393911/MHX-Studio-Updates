# Windows x64 独立实机测试包

业务基础：`c8411b662eec80ab3847f589fe1e16049e607a9f`。
本包仅额外增加测试构建的数据目录启动保护，不改职业识别、角色切换、游戏/任务、ADB、SQLite 或更新实现。不是正式发行，不推进在线清单。

## 使用方法

1. 完全退出 preview.40 的控制软件，保留安装和数据；模拟器可保持运行。两个控制软件不要同时操作同一批模拟器。
2. 将 ZIP 解压到全新的文件夹，勿放入 preview.40 安装目录。运行 `START_TEST.cmd`，或直接双击 `MHXStudio-Issue2-c8411b6-Test.exe`，两者均使用内置隔离目录。
3. 核对软件日志的“配置位置”为 `%LOCALAPPDATA%\MHXStudioTests\Issue2-c8411b6`。LOCALAPPDATA 未设置时依次使用 APPDATA、系统临时目录作为基目录，后面的专用路径不变。该目录首次使用为空，不自动读取、复制或迁移原版本数据。若需职业/页面标定，在测试版中单独设置。
4. 测试版通过内置构建标识强制使用此专用目录；`--data-dir` 指向其他位置会在读取/创建配置或 SQLite 前拒绝启动。再次打开仍使用同一测试目录，以便验收重启后复用。

## 简明验收

- 设置雷电路径并检测编号 1～5，等待 ADB 身份核验完成。
- 在编号 1 手工打开轻享，选择“发现/绑定游戏”并明确确认。安卓桌面 `com.android.launcher3` 不应成为游戏绑定。
- 让编号 1～5 回到安卓桌面，逐台点“仅启动游戏”：核对打开的是各自的轻享，没有串实例或强制重启游戏。编号 2～5 只有在安装入口和实例身份均核验一致时才复用唯一已确认组件。
- 退出测试软件并重开，五台再次“仅启动游戏”，应无需重复确认已核验绑定。测试目录中的每台绑定必须保留。
- 对编号 4 制造短暂断连（不要清数据、恢复出厂或更换绑定），观察未就绪/明确 offline、开始和恢复时间。掉线时不得点击；恢复身份核验后，原动作如提示“需重新检查页面”，应核对页面后重新发起，而非沿用旧截图自动点击。
- 在绑定/恢复等待期间点停止、关闭选择窗，或切换实例/目标角色；不应保存已取消的绑定或继续启动另一个目标。快速重复点击不得重复启动。
- 本轮只验收“仅启动游戏”，不启动组队、八星、市场、消费或完整自动流程。其余功能保留，但不纳入本次实机通过结论。
- 测试结束正常退出，再启动原 preview.40，核对其职业、配置、进度和 SQLite 未变。不要点击本测试版的在线更新按钮，不手工替换任何原版本 EXE。

## 诊断与构建验证

测试日志：专用数据目录下 `diagnostics\adb-connections.log`、`detection.txt`、`detection.json`。提交反馈时标明实例编号、操作顺序和发生时间；配置及截图可能包含私人信息，请自行检查后再分享。

EXE SHA-256 见同目录 `SHA256SUMS.txt`，源提交、工具链、数据隔离标识及构建结果见 `BUILD-RESULT.json`。

构建方式（Go 1.26.10，Linux shell 交叉构建）：

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-H windowsgui -s -w -X main.version=3.5.0-preview.40-issue2-isolated-test -X main.sourceCommit=$(git rev-parse HEAD) -X main.isolatedTestID=Issue2-c8411b6" \
  -o dist/MHXStudio-Issue2-c8411b6-Test.exe ./cmd/mhxstudio
```

验证包括启动目录隔离/拒绝旧目录/原文件不变的自动回归、控制器包竞态测试、Linux/Windows 静态检查、Windows 全包交叉编译。未在本云环境执行原生 Windows GUI/雷电，实机行为须按上述步骤验收。
