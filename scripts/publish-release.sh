#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行Windows自动测试：GUI、EXE备份替换、配置快照、启动超时/退出回滚、失败版本改写配置后的恢复、SHA篡改拒绝和文件占用保护。

本版用于用户真实电脑测试，保留preview.21既有雷电发现、启动、ADB、游戏包名启动及一键更新。新游戏功能尚未通过用户实机验收。

可测试：保存方案后重新打开软件自动启动五台并登录；按实例、区服、选中槽位、职业核验；固定队组队；八星打符与按职业幻形（缺卡只购买一次，结果不明停止）。不再要求昵称识别或昵称标定。

五台登录窗口的单项测试包括：银锭/功绩查询和四区汇总、聚福激活、仙盟战令/打工簿、当周赛季据点首领识别。每项依赖实际页面核验，未知页面/资源或识别冲突会停止并保存诊断。余额只查询，不赠送、不聊天；订单、发货和消息界面已取消。统计按角色整万银锭和整百功绩取较小值，缺测/过期明确显示，不能把未扫描当作零余额。

明确未完成：据点实战、聚福不足时的绑玉兑换、交易行完整清理及重新上架、全部角色/区服/批次无人值守轮转。遇到这些阶段保持未完成，不猜测点击或假报成功。不能将本周首领识别当作通关。

已执行Windows原生GUI、备份替换、配置/SQLite迁移与故障恢复自动测试；原始游戏货币字段另用原生OCR离线识别验证。模拟器/游戏界面流程测试使用夹具，不等于真实雷电/游戏验收。真实电脑故障回滚仍未验收。

只通过软件内“一键检查更新”升级，保留原配置、雷电路径、职业、包名、页面标定及进度数据。发布后还须匿名验证下载/哈希及preview.19、preview.21公开下载一键升级，全部通过才推进清单。原preview.21发行文件和旧备份不覆盖。
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
