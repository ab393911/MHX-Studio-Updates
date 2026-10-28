package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

const maxConfigBytes int64 = 64 << 20

type ConfigFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type ConfigSnapshot struct {
	Schema int          `json:"schema"`
	Root   string       `json:"source_root"`
	Token  string       `json:"transaction_token"`
	Files  []ConfigFile `json:"files"`
}

func configName(name string) bool {
	return name == "studio.sqlite3" || name == "studio.sqlite3-wal" || name == "studio.sqlite3-shm" || name == "config.json" || name == "ldplayer-discovery.json" || (strings.HasPrefix(name, "profiles/") && strings.HasSuffix(name, ".json"))
}
func configFileLimit(name string) int64 {
	if strings.HasPrefix(name, "studio.sqlite3") {
		return 64 << 20
	}
	return 8 << 20
}
func configPath(root, name string) (string, error) {
	if !configName(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
		return "", errors.New("invalid configuration backup path")
	}
	p := root
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." || part == "" {
			return "", errors.New("invalid configuration path component")
		}
		p = filepath.Join(p, part)
		if st, e := os.Lstat(p); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("configuration symlink is not allowed")
		} else if e != nil && !os.IsNotExist(e) {
			return "", e
		}
	}
	return p, nil
}
func configFiles(root string) ([]string, error) {
	var out []string
	for _, name := range []string{"config.json", "ldplayer-discovery.json", "studio.sqlite3", "studio.sqlite3-wal", "studio.sqlite3-shm"} {
		p, e := configPath(root, name)
		if e != nil {
			return nil, e
		}
		if st, e := os.Stat(p); e == nil {
			if !st.Mode().IsRegular() {
				return nil, fmt.Errorf("not a regular configuration file: %s", name)
			}
			out = append(out, name)
		} else if !os.IsNotExist(e) {
			return nil, e
		}
	}
	dir := filepath.Join(root, "profiles")
	e := filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) && path == dir {
			return nil
		}
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("configuration symlink is not allowed")
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		if configName(name) {
			if _, e = configPath(root, name); e != nil {
				return e
			}
			out = append(out, name)
		}
		if len(out) > 1024 {
			return errors.New("configuration backup exceeds 1024 files")
		}
		return nil
	})
	return out, e
}
func (p Plan) ConfigBackupPath() string {
	return filepath.Join(filepath.Dir(p.Backup), filepath.Base(p.Dir())+"-config")
}

// WithConfig records the data directory; the independent updater copies it only
// after the parent exits and before it changes the main executable.
func WithConfig(p Plan, root string) (Plan, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return p, e
	}
	p.DataRoot = filepath.Clean(abs)
	p.ConfigBackup = p.ConfigBackupPath()
	if e = p.Validate(); e != nil {
		return p, e
	}
	return p, filesafe.JSON(filepath.Join(p.Dir(), "plan.json"), p)
}
func BackupConfig(root, dest, token string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || len(token) != 48 {
		return errors.New("invalid configuration snapshot metadata")
	}
	if st, e := os.Lstat(root); e != nil {
		return e
	} else if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid configuration root")
	}
	files, e := configFiles(root)
	if e != nil {
		return e
	}
	if e = os.Mkdir(dest, 0700); e != nil {
		return e
	}
	m := ConfigSnapshot{Schema: 1, Root: root, Token: token, Files: []ConfigFile{}}
	var total int64
	for _, name := range files {
		src, e := configPath(root, name)
		if e != nil {
			return e
		}
		st, e := os.Stat(src)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() || st.Size() > configFileLimit(name) {
			return fmt.Errorf("configuration file too large or invalid: %s", name)
		}
		total += st.Size()
		if total > maxConfigBytes {
			return errors.New("configuration backup exceeds 64 MiB")
		}
		dst := filepath.Join(dest, "files", filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = filesafe.CopyNew(src, dst); e != nil {
			return e
		}
		hash, e := Hash(dst)
		if e != nil {
			return e
		}
		original, e := Hash(src)
		if e != nil || hash != original {
			return errors.New("configuration changed while backing up")
		}
		m.Files = append(m.Files, ConfigFile{name, hash, st.Size()})
	}
	return filesafe.JSON(filepath.Join(dest, "manifest.json"), m)
}
func ReadConfigBackup(dir, root, token string) (ConfigSnapshot, error) {
	var m ConfigSnapshot
	if e := filesafe.ReadJSON(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return m, e
	}
	if m.Schema != 1 || m.Root != root || m.Token != token || len(m.Files) > 1024 {
		return m, errors.New("configuration snapshot metadata mismatch")
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if _, e := configPath(root, f.Path); e != nil {
			return m, e
		}
		if seen[strings.ToLower(f.Path)] || !hashRE.MatchString(f.SHA256) || f.Size < 0 || f.Size > configFileLimit(f.Path) {
			return m, errors.New("invalid configuration snapshot entry")
		}
		seen[strings.ToLower(f.Path)] = true
		total += f.Size
		if total > maxConfigBytes {
			return m, errors.New("configuration snapshot too large")
		}
		path := filepath.Join(dir, "files", filepath.FromSlash(f.Path))
		st, e := os.Lstat(path)
		if e != nil {
			return m, e
		}
		if !st.Mode().IsRegular() || st.Size() != f.Size {
			return m, errors.New("configuration snapshot file mismatch")
		}
		if e = Verify(path, f.SHA256); e != nil {
			return m, e
		}
	}
	return m, nil
}

// Restore stages complete copies first, then quarantines current configuration
// on the same data volume. No old backup or failed migration file is deleted.
func RestoreConfig(dir, root, token string) error {
	m, e := ReadConfigBackup(dir, root, token)
	if e != nil {
		return e
	}
	current, e := configFiles(root)
	if e != nil {
		return e
	}
	stage, e := os.MkdirTemp(root, ".mhx-restore-")
	if e != nil {
		return e
	}
	for _, f := range m.Files {
		dst := filepath.Join(stage, filepath.FromSlash(f.Path))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = filesafe.CopyNew(filepath.Join(dir, "files", filepath.FromSlash(f.Path)), dst); e != nil {
			return e
		}
		if e = Verify(dst, f.SHA256); e != nil {
			return e
		}
	}
	quarantine := filepath.Join(root, ".mhx-rollback", token+"-"+NewToken())
	for _, name := range current {
		src, e := configPath(root, name)
		if e != nil {
			return e
		}
		dst := filepath.Join(quarantine, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = os.Rename(src, dst); e != nil {
			return e
		}
	}
	for _, f := range m.Files {
		dst, e := configPath(root, f.Path)
		if e != nil {
			return e
		}
		if _, e = os.Lstat(dst); !os.IsNotExist(e) {
			return errors.New("configuration restore destination already exists")
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = os.Rename(filepath.Join(stage, filepath.FromSlash(f.Path)), dst); e != nil {
			return e
		}
	}
	return nil
}

// Legacy preview.15 cannot request a pre-replacement snapshot. The new GUI
// creates it before acknowledging startup health or changing any settings.
func EnsureStartupConfigBackup(p Plan, root string) (string, error) {
	dest := p.ConfigBackupPath()
	if p.DataRoot != "" && p.DataRoot != root {
		return "", errors.New("startup configuration directory mismatch")
	}
	if _, e := os.Stat(filepath.Join(dest, "manifest.json")); e == nil {
		_, e = ReadConfigBackup(dest, root, p.Token)
		return dest, e
	}
	if p.ConfigBackup != "" {
		return "", errors.New("pre-update configuration snapshot is missing")
	}
	return dest, BackupConfig(root, dest, p.Token)
}
