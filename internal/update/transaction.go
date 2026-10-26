package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

type Plan struct {
	Target    string `json:"target"`
	Stage     string `json:"stage"`
	Backup    string `json:"backup"`
	OldSHA    string `json:"old_sha256"`
	NewSHA    string `json:"new_sha256"`
	Version   string `json:"version"`
	Token     string `json:"token"`
	ParentPID int    `json:"parent_pid"`
}
type Receipt struct {
	Version string `json:"version"`
	Token   string `json:"token"`
	SHA256  string `json:"sha256"`
}
type Result struct {
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Backup  string `json:"backup"`
	Version string `json:"version"`
}

// Runtime owns OS-specific child process and parent-exit handling.
type Runtime interface {
	WaitParent(context.Context, int) error
	Launch(string, ...string) (Child, error)
}
type Child interface{ Stop() error }
type Installer struct {
	Runtime       Runtime
	Rename        func(string, string) error
	HealthTimeout time.Duration
}

func NewToken() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func NewPlan(target, stage, version, newHash string) (Plan, error) {
	target, e := filepath.Abs(target)
	if e != nil {
		return Plan{}, e
	}
	stage, e = filepath.Abs(stage)
	if e != nil {
		return Plan{}, e
	}
	old, e := Hash(target)
	if e != nil {
		return Plan{}, e
	}
	id := filepath.Base(filepath.Dir(stage))
	backupDir := filepath.Join(filepath.Dir(target), ".mhx-backups")
	if e = os.MkdirAll(backupDir, 0700); e != nil {
		return Plan{}, e
	}
	p := Plan{Target: target, Stage: stage, Backup: filepath.Join(backupDir, id+"-"+filepath.Base(target)), OldSHA: old, NewSHA: newHash, Version: version, Token: NewToken(), ParentPID: os.Getpid()}
	if e = p.Validate(); e != nil {
		return Plan{}, e
	}
	if e = filesafe.JSON(filepath.Join(p.Dir(), "plan.json"), p); e != nil {
		return Plan{}, e
	}
	return p, nil
}
func (p Plan) Dir() string     { return filepath.Dir(p.Stage) }
func (p Plan) Validate() error { return p.validate(false) }
func (p Plan) validate(recovery bool) error {
	root := filepath.Dir(p.Target)
	dir := p.Dir()
	for _, s := range []string{p.Target, p.Stage, p.Backup} {
		if !filepath.IsAbs(s) || filepath.Clean(s) != s {
			return errors.New("transaction paths must be clean and absolute")
		}
	}
	if filepath.Dir(filepath.Dir(dir)) != root || filepath.Base(filepath.Dir(dir)) != ".mhx-updates" || !strings.HasPrefix(filepath.Base(dir), "tx-") {
		return errors.New("stage is outside a same-folder transaction")
	}
	if filepath.Base(p.Stage) != "package.exe" || filepath.Dir(p.Backup) != filepath.Join(root, ".mhx-backups") || filepath.Base(p.Backup) != filepath.Base(dir)+"-"+filepath.Base(p.Target) {
		return errors.New("invalid stage or backup path")
	}
	if !strings.EqualFold(filepath.Ext(p.Target), ".exe") || !hashRE.MatchString(p.OldSHA) || !hashRE.MatchString(p.NewSHA) || len(p.Token) != 48 || p.ParentPID < 1 {
		return errors.New("invalid transaction metadata")
	}
	if _, e := hex.DecodeString(p.Token); e != nil {
		return e
	}
	if _, e := versionParts(p.Version); e != nil {
		return e
	}
	paths := []string{root, filepath.Dir(dir), dir, filepath.Dir(p.Backup)}
	if recovery {
		paths = append(paths, p.Backup)
	} else {
		paths = append(paths, p.Target, p.Stage)
	}
	for _, s := range paths {
		v, e := os.Lstat(s)
		if e != nil {
			return e
		}
		if v.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink/reparse transaction paths are not allowed")
		}
	}
	return nil
}
func (i Installer) rename(a, b string) error {
	if _, e := os.Lstat(b); e == nil {
		return fmt.Errorf("refusing to overwrite existing file %s", b)
	} else if !os.IsNotExist(e) {
		return e
	}
	if i.Rename != nil {
		return i.Rename(a, b)
	}
	return os.Rename(a, b)
}
func (i Installer) result(p Plan, status, detail string) error {
	return filesafe.JSON(filepath.Join(p.Dir(), "result.json"), Result{status, detail, p.Backup, p.Version})
}
func (i Installer) Install(ctx context.Context, p Plan) error {
	if i.Runtime == nil {
		return errors.New("missing update runtime")
	}
	if e := p.Validate(); e != nil {
		return e
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	e := i.Runtime.WaitParent(wait, p.ParentPID)
	cancel()
	if e != nil {
		return errors.Join(e, i.result(p, "unchanged", e.Error()))
	}
	// Both files are rechecked after the parent exits, immediately before mutation.
	if e = Verify(p.Target, p.OldSHA); e != nil {
		return errors.Join(e, i.result(p, "unchanged", e.Error()))
	}
	if e = Verify(p.Stage, p.NewSHA); e != nil {
		return errors.Join(e, i.result(p, "unchanged", e.Error()))
	}
	if e = ValidateEXE(p.Stage); e != nil {
		return errors.Join(e, i.result(p, "unchanged", e.Error()))
	}
	if e = i.result(p, "prepared", ""); e != nil {
		return e
	}
	if e = i.rename(p.Target, p.Backup); e != nil {
		return errors.Join(e, i.result(p, "unchanged", e.Error()))
	}
	if e = i.result(p, "backed_up", ""); e != nil {
		return i.rollback(p, e)
	}
	if e = i.rename(p.Stage, p.Target); e != nil {
		return i.rollback(p, e)
	}
	if e = i.result(p, "awaiting_health", ""); e != nil {
		return i.rollback(p, e)
	}
	receiptPath := filepath.Join(p.Dir(), "health.json")
	child, e := i.Runtime.Launch(p.Target, "--health-receipt", receiptPath, "--health-token", p.Token, "--health-version", p.Version)
	if e != nil {
		return i.rollback(p, fmt.Errorf("launch new EXE: %w", e))
	}
	timeout := i.HealthTimeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	health, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var r Receipt
		if filesafe.ReadJSON(receiptPath, &r) == nil && r.Version == p.Version && r.Token == p.Token && strings.EqualFold(r.SHA256, p.NewSHA) {
			if e = Verify(p.Target, p.NewSHA); e == nil {
				return i.result(p, "committed", "window created and message loop acknowledged; backup retained")
			}
		}
		select {
		case <-health.Done():
			if e = child.Stop(); e != nil {
				return errors.Join(e, i.result(p, "recovery_required", "new process could not be stopped; backup retained"))
			}
			return i.rollback(p, errors.New("new version did not pass GUI startup health check"))
		case <-ticker.C:
		}
	}
}
func (i Installer) rollback(p Plan, cause error) error {
	if e := Verify(p.Backup, p.OldSHA); e != nil {
		return errors.Join(cause, e, i.result(p, "recovery_required", "backup verification failed"))
	}
	if _, e := os.Lstat(p.Target); e == nil {
		if e = i.rename(p.Target, filepath.Join(p.Dir(), "failed.exe")); e != nil {
			return errors.Join(cause, e, i.result(p, "recovery_required", "could not move failed EXE aside"))
		}
	} else if !os.IsNotExist(e) {
		return errors.Join(cause, e)
	}
	restored := filepath.Join(p.Dir(), "restore.exe")
	if e := filesafe.CopyNew(p.Backup, restored); e != nil {
		return errors.Join(cause, e, i.result(p, "recovery_required", e.Error()))
	}
	if e := Verify(restored, p.OldSHA); e != nil {
		return errors.Join(cause, e, i.result(p, "recovery_required", e.Error()))
	}
	if e := i.rename(restored, p.Target); e != nil {
		return errors.Join(cause, e, i.result(p, "recovery_required", e.Error()))
	}
	e := i.result(p, "rolled_back", cause.Error())
	_, launchErr := i.Runtime.Launch(p.Target, "--update-result", filepath.Join(p.Dir(), "result.json"))
	return errors.Join(cause, e, launchErr)
}

// Recover only fills a missing target; it never overwrites a program on disk.
// Useful after power loss between the two renames. Backups stay intact.
func (i Installer) Recover(p Plan) error {
	if e := p.validate(true); e != nil {
		return e
	}
	if _, e := os.Lstat(p.Target); !os.IsNotExist(e) {
		return errors.New("recovery requires the target EXE to be absent")
	}
	if e := Verify(p.Backup, p.OldSHA); e != nil {
		return e
	}
	restored := filepath.Join(p.Dir(), "recover-"+NewToken()+".exe")
	if e := filesafe.CopyNew(p.Backup, restored); e != nil {
		return e
	}
	if e := Verify(restored, p.OldSHA); e != nil {
		return e
	}
	if e := i.rename(restored, p.Target); e != nil {
		return e
	}
	return i.result(p, "rolled_back", "missing target recovered from retained backup")
}
