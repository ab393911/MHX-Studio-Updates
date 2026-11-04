package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

var iconWriteMu sync.Mutex

func iconScopePath(root string, index int, shared bool) string {
	id := strconv.Itoa(index)
	if shared {
		id = "shared"
	}
	return filepath.Join(root, "profiles", "icon-drafts", id+".json")
}
func readIconScope(root string, index int, shared bool) (automation.IconRules, string, error) {
	r := automation.NewIconRules()
	raw, err := os.ReadFile(iconScopePath(root, index, shared))
	if os.IsNotExist(err) {
		return r, "", nil
	}
	if err != nil {
		return r, "", err
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(raw))
	r = automation.IconRules{}
	if err = json.Unmarshal(raw, &r); err != nil {
		return r, sha, err
	}
	return r, sha, r.Validate()
}
func loadIcons(root string, index int) (automation.IconRules, error) {
	shared, _, err := readIconScope(root, index, true)
	if err != nil {
		return shared, err
	}
	local, _, err := readIconScope(root, index, false)
	if err != nil {
		return local, err
	}
	for k, v := range local.Profile.Pages {
		shared.Profile.Pages[k] = v
	}
	for k, v := range local.Profile.Buttons {
		shared.Profile.Buttons[k] = v
	}
	if len(local.MainIcons) > 0 {
		shared.MainIcons = local.MainIcons
		shared.MinIcons = local.MinIcons
		shared.StableFrames = local.StableFrames
	}
	if len(local.Blockers) > 0 {
		shared.Blockers = local.Blockers
	}
	return shared, shared.Validate()
}

func saveIconScope(root string, index int, shared bool, r automation.IconRules, expectedSHA string) error {
	return writeIconScope(root, index, shared, r, expectedSHA, filesafe.Write)
}
func writeIconScope(root string, index int, shared bool, r automation.IconRules, expectedSHA string, write func(string, []byte, os.FileMode) error) error {
	if !shared && index < 0 {
		return fmt.Errorf("invalid icon instance")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	iconWriteMu.Lock()
	defer iconWriteMu.Unlock()
	path := iconScopePath(root, index, shared)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	actual := ""
	if err == nil {
		actual = fmt.Sprintf("%x", sha256.Sum256(old))
	}
	if actual != expectedSHA {
		return fmt.Errorf("图标草稿已被修改；请重新载入，未覆盖磁盘内容")
	}
	previous := automation.NewIconRules()
	_ = json.Unmarshal(old, &previous)
	if r.Revision < previous.Revision {
		r.Revision = previous.Revision
	}
	r.Revision++
	r.Profile = withRuleRevisions(previous.Profile, r.Profile)
	if r.Revision < 1 {
		r.Revision = 1
	}
	if actual != "" {
		history := filepath.Join(root, "profiles", "icon-drafts", "history")
		if err = os.MkdirAll(history, 0700); err != nil {
			return err
		}
		name := fmt.Sprintf("%s-%d-%s.json", filepath.Base(path), time.Now().UnixNano(), actual[:12])
		if err = write(filepath.Join(history, name), old, 0600); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return write(path, append(raw, '\n'), 0600)
}

// Compose only edits a preview; the GUI still requires explicit Save/confirmation.
func composeIconMain(r automation.IconRules) (automation.IconRules, error) {
	var ids []string
	for _, candidate := range automation.IconCandidates() {
		if _, ok := r.Profile.Pages[candidate.ID]; ok {
			ids = append(ids, candidate.ID)
		}
	}
	r.MainIcons = ids
	r.MinIcons = 2
	r.StableFrames = 2
	return r, r.Validate()
}
