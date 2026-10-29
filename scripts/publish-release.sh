#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行Windows自动测试：GUI、EXE备份替换、配置快照、启动超时/退出回滚、失败版本改写配置后的恢复、SHA篡改拒绝和文件占用保护。

基于已实机通过的preview.19，集中接入：截图候选识别与广告外框关闭、现有角色标定优先、自选任意五台启动和已绑定游戏启动、持久化小组/区服/批次计划、本地订单与额度核对、并发配额与不明交易保护、截图保留策略以及海嘟嘟战术预检。

需求已删除锁灵塔、整个日课、群英榜、其他福利红点、主线和战力任务；旧记录保留审计，阶段编号不重排，订单流水不清除。保留仙盟战令与打工簿、聚福激活和交易行。SQLite迁移纳入启动健康检查。

真实游戏自动任务尚未开放：自动组队、八星/聚福/据点、交易行、战令等目前具备判断核心或候选页面，仍缺完整读屏执行与实机闭环；海嘟嘟不进行实际战斗，银锭订单不实际发货或发消息。职业候选存在选中行歧义，保留人工绑定；角色身份未核验只能报告部分成功。详见源码docs/HANDOFF_ACCEPTANCE_20261029.md。

Windows自动测试覆盖基础GUI/更新回滚、破坏订单数据库后的恢复、自选五台和包名启动、计划与订单保存重启、额度核对；SQLite并发/不明结果/进程中断、市场/战令安全规则和回合策略另有单元测试。真实故障回滚仍未验收。交接183张截图已做离线SHA验证和候选回放，不等于实机成功。

用户只点软件内“一键检查更新”；保留配置、职业、包名绑定、页面标定及未知字段。新进度/订单库在今后更新中随主程序退出、关闭并检查点后备份，失败恢复。preview.19本来没有此新数据库，第一跳主要验证原JSON/标定；数据库前置备份/回滚用当前源码旧版本号基线及Windows故障注入验证。

只有实际运行已发布preview.19、一键升级到本版、匿名下载/哈希、原设置保留、24实例及单台/五台启动ADB回归全部通过，才推进默认清单。所有新游戏功能仍需用户单台实机验证后再五台。发行资产和旧备份不覆盖。
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
