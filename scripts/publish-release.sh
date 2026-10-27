#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行Windows自动测试：GUI、EXE备份替换、配置快照、启动超时/退出回滚、失败版本改写配置后的恢复、SHA篡改拒绝和文件占用保护。

本轮只验收一键更新，保留用户已在真实Windows验收的preview.15雷电检测和启动实现。新版点击“一键检查更新”后自动检查、下载、SHA-256校验、备份、替换、重启和显示结果，无需浏览器、ZIP或命令。没有更新时显示“当前已是最新版本”。

首次从preview.15升级仍需旧EXE原有的一次确认；旧15的配置快照由新GUI在健康确认前补建。后续版本取消二次确认，并在替换前备份配置。发布通道只有在真实已发布15的升级及新更新器零确认测试均通过后才推进；后两份JSON报告随后追加。升级后24实例检测/启动使用CI接口模拟，仍需用户新版本实机验收。

用户当前只需在preview.15点击更新并完成首次旧确认，等待自动重启。保留所有旧版和备份，不要求手动解压。用户电脑升级验收完成前暂停其他游戏功能开发。发行版不覆盖已有发布资产。
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
