#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行Windows自动测试：GUI、EXE备份替换、配置快照、启动超时/退出回滚、失败版本改写配置后的恢复、SHA篡改拒绝和文件占用保护。

本轮新增单台游戏启动与指定角色登录测试入口，复用已有页面标定。首次目标游戏不能唯一确认时，由用户在图形界面确认绑定，不试启动其它应用。未知/冲突页面停止；目标身份无法核验时不报告完全成功。真实游戏画面模板未随程序提供，使用本机已保存的标定，缺项请按“标定指引”补齐。

preview.17由用户真实Windows验收：在线升级、D:\LDPlayer14自动发现、24实例清单、单台与完全关闭后五台批量启动、ADB验证。保持基础实现，新增游戏功能尚待实机验收；不含任务、战斗、换区、创建角色或五台游戏自动化。真实机器故障回滚仍未验收。

所有后续版本经软件内“一键检查更新”交付，不要求手动下载、解压或替换。保留雷电路径、角色、游戏绑定、页面标定和未知配置；替换前备份并校验，启动健康检查失败自动恢复旧版与配置。每次发布保留Windows失败回滚与模拟器回归测试。

默认清单仅在已发布preview.17到本版的实际单击GUI升级、配置原字节保留、重启可见、24实例及单台/五台启动与ADB模拟接口回归通过后推进。Windows游戏测试使用合成截图，不能等同真实轻享通过。完整状态与测试步骤见源码docs/SINGLE_GAME_ACCEPTANCE.md。发行版不覆盖已有发布资产。
EOF
gh release create "$tag" dist/MHXStudio.exe dist/MHXStudio-Windows-x64.zip dist/update.json dist/SHA256SUMS.txt dist/windows-tests.json --repo "$GITHUB_REPOSITORY" --target "$GITHUB_SHA" --title "MHX Studio ${MHX_VERSION} (preview)" --notes-file "$notes_file" --prerelease --draft
gh release edit "$tag" --repo "$GITHUB_REPOSITORY" --draft=false
manifest="https://github.com/${GITHUB_REPOSITORY}/releases/download/${tag}/update.json"
verified=false
for attempt in 1 2 3 4 5 6; do
  if go run ./cmd/release -verify "$manifest" -commit "$GITHUB_SHA" -version "$MHX_VERSION" -report dist/download-verification.json; then verified=true; break; fi
  sleep 5
done
if [ "$verified" != true ]; then
  gh release edit "$tag" --repo "$GITHUB_REPOSITORY" --draft=true
  exit 1
fi
# Channel promotion is a separate job after the live GUI update test.
