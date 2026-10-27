#!/usr/bin/env bash
set -euo pipefail
# Assert the live GUI report is for these exact release bytes and source.
jq -e --arg version "$MHX_VERSION" --arg sha "$GITHUB_SHA" '.passed == true and .version == $version and .source_commit == $sha and .configuration_byte_identical == true and .windows_game_tested == false' dist/live-gui-update.json > /dev/null
jq -e --arg version "$MHX_VERSION" --arg sha "$GITHUB_SHA" '.passed == true and .version == $version and .source_commit == $sha and .one_click_without_confirmation == true and .confirmation_clicks == 0 and .configuration_snapshot_verified == true and .post_upgrade_24_instance_detection_fixture == true and .post_upgrade_start_and_adb_fixture == true' dist/one-click-gui-update.json > /dev/null
jq -e '.new_gui_visible == true' dist/live-gui-update.json > /dev/null
jq -e '.new_gui_visible == true' dist/one-click-gui-update.json > /dev/null
jq -e '.baseline_published == true and .previous_version == "3.5.0-preview.17" and .one_click_without_confirmation == true and .confirmation_clicks == 0 and .post_upgrade_five_launch_and_adb_fixture == true and .game_settings_byte_preserved == true' dist/live-gui-update.json > /dev/null
jq -e '.passed == true and .startup_timeout.passed == true and .immediate_exit.passed == true and .config_migration_exit.passed == true and .sha_tamper.passed == true and .sharing_violation.passed == true and .emulator_gui_fixture.cold_five_launch_and_adb_verified == true and .single_game_gui_fixture.passed == true and .single_game_gui_fixture.real_game_tested == false' dist/windows-tests.json > /dev/null
go run ./cmd/release -verify "https://github.com/${GITHUB_REPOSITORY}/releases/download/v${MHX_VERSION}/update.json" -commit "$GITHUB_SHA" -version "$MHX_VERSION"
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
  if go run ./cmd/release -verify 'https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json' -commit "$GITHUB_SHA" -version "$MHX_VERSION" -report dist/download-verification.json; then
    gh release upload "v${MHX_VERSION}" dist/live-gui-update.json dist/one-click-gui-update.json dist/download-verification.json --repo "$GITHUB_REPOSITORY"
    exit 0
  fi
  sleep 5
done
exit 1
