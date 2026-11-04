package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"os"
	"path/filepath"
	"strings"
)

func sharedProfilePath(root string) string { return filepath.Join(root, "profiles", "shared.json") }
func readProfile(path string) (automation.Profile, error) {
	p := automation.NewProfile()
	err := filesafe.ReadJSON(path, &p)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, p.Validate()
}

// Shared rules are immutable inputs to each operation. Instance overrides win;
// no task state, frame cache or device identity is shared here.
func loadProfile(root string, index int) (automation.Profile, error) {
	shared, err := readProfile(sharedProfilePath(root))
	if err != nil {
		return shared, fmt.Errorf("公共规则无法读取，停止识别：%w", err)
	}
	local, err := loadLocalProfile(root, index)
	if err != nil {
		return local, err
	}
	for k, v := range local.Pages {
		shared.Pages[k] = v
	}
	for k, v := range local.Buttons {
		shared.Buttons[k] = v
	}
	return shared, shared.Validate()
}
func saveRuleLibrary(root string, index int, p automation.Profile, shared bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	path := profilePath(root, index)
	if shared {
		path = sharedProfilePath(root)
	}
	old, _ := readProfile(path)
	p = withRuleRevisions(old, p)
	if !shared {
		return saveProfile(root, index, p)
	}
	// Reuse the same validate/backup/atomic-write path; "shared" has no device state.
	return saveProfileFile(root, sharedProfilePath(root), "shared", p)
}
func withRuleRevisions(old, p automation.Profile) automation.Profile {
	pages := make(map[string]automation.Page, len(p.Pages))
	for k, v := range p.Pages {
		pages[k] = v
	}
	p.Pages = pages
	for key, page := range p.Pages {
		previous, exists := old.Pages[key]
		a, b := previous, page
		a.Revision = 0
		b.Revision = 0
		before, _ := json.Marshal(a)
		after, _ := json.Marshal(b)
		if exists && bytes.Equal(before, after) {
			page.Revision = previous.Revision
			if page.Revision < 1 {
				page.Revision = 1
			}
		} else {
			previousRevision := previous.Revision
			if exists && previousRevision < 1 {
				previousRevision = 1
			}
			page.Revision = previousRevision + 1
			if page.Revision < 1 {
				page.Revision = 1
			}
		}
		p.Pages[key] = page
	}
	return p
}
func restoreRuleLibrary(root string, index int, path string, shared bool) error {
	// The GUI supplies an explicitly chosen file and asks for confirmation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p := automation.NewProfile()
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return err
	}
	return saveRuleLibrary(root, index, p, shared)
}
func ruleFileHash(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func validateExportDestination(root, target string) error {
	base, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	dest, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	// Resolve existing directories so a junction/symlink cannot redirect an export
	// into application data. A different Windows volume is a valid export location.
	if resolved, e := filepath.EvalSymlinks(base); e == nil {
		base = resolved
	} else {
		return e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(dest))
	if e != nil {
		return e
	}
	dest = filepath.Join(parent, filepath.Base(dest))
	if !strings.EqualFold(filepath.VolumeName(base), filepath.VolumeName(dest)) {
		return nil
	}
	rel, err := filepath.Rel(base, dest)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("请选择程序数据目录之外的位置，避免覆盖现有配置或标定")
	}
	return nil
}
