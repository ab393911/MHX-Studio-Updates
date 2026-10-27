#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行 Windows 自动测试：GUI 窗口和消息循环、实际 EXE 备份替换、启动超时回滚、哈希篡改拒绝安装、文件占用保护和配置原字节保留。

雷电自动发现修复：启动时搜索管理程序、独立ADB和本机ADB服务；包含D:\\LDPlayer14候选但不假设存在；有效目录单独保存；检测详情列出已检查路径和失败原因。没有管理程序时也能按真实ADB序列号显示设备，实例编号不明确时停用游戏控制。保留7/10列列表解析及单台/本组启动。Windows测试使用命令行和ADB协议测试替身，不代表真实雷电实机已验证。

真实 Windows + 雷电 + 游戏登录流程尚未验收。需按 TESTING.md 对每台实例标定并先测单机，再测五开。不含战斗或自动换区。

请解压到新文件夹，保留 V3.3/V3.4。发行版不会覆盖已有发布资产。
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
