package update

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultManifestURL = "https://raw.githubusercontent.com/ab393911/MHX-Studio-Updates/updates/preview.json"
const MaxPackageSize int64 = 128 << 20

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var hashRE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type Manifest struct {
	Schema        int    `json:"schema_version"`
	Version       string `json:"version"`
	Platform      string `json:"platform"`
	URL           string `json:"download_url"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Notes         string `json:"notes"`
	SourceCommit  string `json:"source_commit"`
	WindowsTested bool   `json:"windows_game_tested"`
}

func versionParts(s string) ([]string, error) {
	p := versionRE.FindStringSubmatch(s)
	if p == nil {
		return nil, fmt.Errorf("invalid version %q", s)
	}
	for _, v := range strings.Split(p[4], ".") {
		if len(v) > 1 && v[0] == '0' {
			if _, e := strconv.ParseUint(v, 10, 64); e == nil {
				return nil, errors.New("numeric prerelease has a leading zero")
			}
		}
	}
	return p, nil
}
func numericCompare(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func Compare(a, b string) (int, error) {
	x, e := versionParts(a)
	if e != nil {
		return 0, e
	}
	y, e := versionParts(b)
	if e != nil {
		return 0, e
	}
	for i := 1; i <= 3; i++ {
		if c := numericCompare(x[i], y[i]); c != 0 {
			return c, nil
		}
	}
	if x[4] == y[4] {
		return 0, nil
	}
	if x[4] == "" {
		return 1, nil
	}
	if y[4] == "" {
		return -1, nil
	}
	xa, ya := strings.Split(x[4], "."), strings.Split(y[4], ".")
	digits := regexp.MustCompile(`^[0-9]+$`)
	for i := 0; i < len(xa) && i < len(ya); i++ {
		an, bn := digits.MatchString(xa[i]), digits.MatchString(ya[i])
		var c int
		switch {
		case an && bn:
			c = numericCompare(xa[i], ya[i])
		case an:
			c = -1
		case bn:
			c = 1
		default:
			c = strings.Compare(xa[i], ya[i])
		}
		if c != 0 {
			return c, nil
		}
	}
	if len(xa) < len(ya) {
		return -1, nil
	}
	return 1, nil
}
func HTTPS(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("update URL must be an absolute HTTPS URL without credentials or fragments")
	}
	return nil
}
func (m Manifest) Validate() error {
	if m.Schema != 1 || m.Platform != "windows-amd64" {
		return errors.New("unsupported manifest schema or platform")
	}
	if _, e := versionParts(m.Version); e != nil {
		return e
	}
	if e := HTTPS(m.URL); e != nil {
		return e
	}
	if !hashRE.MatchString(m.SHA256) {
		return errors.New("invalid SHA-256")
	}
	if m.Size < 1024 || m.Size > MaxPackageSize {
		return errors.New("package size outside allowed range")
	}
	return nil
}
func Client() *http.Client {
	return &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 7 {
			return errors.New("too many redirects")
		}
		return HTTPS(req.URL.String())
	}}
}
func get(ctx context.Context, c *http.Client, raw string) (*http.Response, error) {
	if e := HTTPS(raw); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "MHXStudio-Updater/1")
	req.Header.Set("Cache-Control", "no-cache")
	r, e := c.Do(req)
	if e != nil {
		return nil, e
	}
	if e = HTTPS(r.Request.URL.String()); e != nil {
		r.Body.Close()
		return nil, e
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", r.StatusCode, r.Request.URL.Host)
	}
	return r, nil
}
func Check(ctx context.Context, c *http.Client, raw, current string) (Manifest, bool, error) {
	var m Manifest
	r, e := get(ctx, c, raw)
	if e != nil {
		return m, false, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if e != nil {
		return m, false, e
	}
	if len(b) > 64<<10 {
		return m, false, errors.New("manifest exceeds 64 KiB")
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, false, e
	}
	if e = m.Validate(); e != nil {
		return m, false, e
	}
	n, e := Compare(m.Version, current)
	return m, n > 0, e
}
func Hash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func Verify(path, hash string) error {
	actual, e := Hash(path)
	if e != nil {
		return e
	}
	if !strings.EqualFold(actual, hash) {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", hash, actual)
	}
	return nil
}
func ValidateEXE(path string) error {
	p, e := pe.Open(path)
	if e != nil {
		return fmt.Errorf("invalid Windows EXE: %w", e)
	}
	defer p.Close()
	if p.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("package is not Windows x64")
	}
	if _, ok := p.OptionalHeader.(*pe.OptionalHeader64); !ok {
		return errors.New("PE32+ header missing")
	}
	if len(p.Sections) == 0 {
		return errors.New("EXE has no sections")
	}
	return nil
}
func Download(ctx context.Context, c *http.Client, m Manifest, dir string) (string, error) {
	if e := m.Validate(); e != nil {
		return "", e
	}
	r, e := get(ctx, c, m.URL)
	if e != nil {
		return "", e
	}
	defer r.Body.Close()
	if r.ContentLength >= 0 && r.ContentLength != m.Size {
		return "", errors.New("download content length disagrees with manifest")
	}
	f, e := os.OpenFile(filepath.Join(dir, "package.part"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return "", e
	}
	name := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(name)
		}
	}()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, m.Size+1))
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return "", e
	}
	if n != m.Size {
		return "", fmt.Errorf("download incomplete or oversized: %d/%d", n, m.Size)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		return "", errors.New("download SHA-256 mismatch; installed EXE is untouched")
	}
	if e = ValidateEXE(name); e != nil {
		return "", e
	}
	dst := filepath.Join(dir, "package.exe")
	if e = os.Rename(name, dst); e != nil {
		return "", e
	}
	ok = true
	return dst, nil
}
