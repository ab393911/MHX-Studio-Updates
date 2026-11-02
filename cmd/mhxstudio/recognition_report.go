package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"sort"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
)

type recognitionPage struct {
	automation.PageScore
	Source string `json:"source"`
}
type recognitionReport struct {
	ProfilePath   string            `json:"profile_path"`
	ProfileSHA    string            `json:"profile_file_sha256"`
	LoadedAt      string            `json:"loaded_at"`
	ImageBounds   image.Rectangle   `json:"image_bounds"`
	ScreenshotSHA string            `json:"screenshot_sha256"`
	CacheUsed     bool              `json:"cache_used"`
	Pages         []recognitionPage `json:"pages"`
	Matches       []string          `json:"matches"`
}

// Read one immutable file snapshot after taking the fresh screenshot. No profile
// or screenshot cache participates. The file digest identifies exactly what ran.
func inspectRecognition(root string, index int, img image.Image) (recognitionReport, error) {
	if img == nil {
		return recognitionReport{}, fmt.Errorf("missing fresh screenshot")
	}
	report := recognitionReport{ProfilePath: profilePath(root, index), LoadedAt: time.Now().Format(time.RFC3339Nano), ImageBounds: img.Bounds(), CacheUsed: false}
	local := automation.NewProfile()
	raw, err := os.ReadFile(report.ProfilePath)
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	if err == nil {
		report.ProfileSHA = fmt.Sprintf("%x", sha256.Sum256(raw))
		if err = json.Unmarshal(raw, &local); err != nil {
			return report, err
		}
	}
	if err = local.Validate(); err != nil {
		return report, err
	}
	catalog, err := evidence.Load()
	if err != nil {
		return report, err
	}
	effective := catalog.Profile(local)
	keys := make([]string, 0, len(effective.Pages))
	for key := range effective.Pages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		origin := "built-in"
		if _, ok := local.Pages[key]; ok {
			origin = "local"
		}
		score := effective.Explain(img, key)
		report.Pages = append(report.Pages, recognitionPage{score, origin})
		if effective.Require(img, key) == nil {
			report.Matches = append(report.Matches, key)
		}
	}
	return report, nil
}
