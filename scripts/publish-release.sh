#!/usr/bin/env bash
set -euo pipefail
tag="v${MHX_VERSION}"
notes_file="${RUNNER_TEMP}/mhx-release-notes.md"
cat > "$notes_file" <<EOF
Windows x64 原生图形界面测试版 ${MHX_VERSION}

来自提交 ${GITHUB_SHA}。已执行 Windows 自动测试：GUI 窗口和消息循环、实际 EXE 备份替换、启动超时回滚、哈希篡改拒绝安装、文件占用保护和配置原字节保留。

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
# The updates ref is a separate channel branch. Main and every previous release remain intact.
if ! gh api "repos/${GITHUB_REPOSITORY}/git/ref/heads/updates" > /dev/null 2>&1; then
  gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" -f ref='refs/heads/updates' -f sha="$GITHUB_SHA" > /dev/null
fi
existing_sha="$(gh api "repos/${GITHUB_REPOSITORY}/contents/preview.json?ref=updates" --jq .sha 2>/dev/null || true)"
content="$(base64 -w 0 dist/update.json)"
request="${RUNNER_TEMP}/mhx-channel.json"
jq -n --arg content "$content" --arg sha "$existing_sha" --arg message "Publish verified ${MHX_VERSION} preview channel" '{branch:"updates",message:$message,content:$content} + (if $sha == "" then {} else {sha:$sha} end)' > "$request"
gh api --method PUT "repos/${GITHUB_REPOSITORY}/contents/preview.json" --input "$request" > /dev/null
# Confirm the exact client-facing channel, without a GitHub token.
for attempt in 1 2 3 4 5 6; do
  if go run ./cmd/release -verify 'https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json' -commit "$GITHUB_SHA" -version "$MHX_VERSION" -report dist/download-verification.json; then exit 0; fi
  sleep 5
done
exit 1
