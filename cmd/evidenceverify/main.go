package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
)

func fail(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func main() {
	root := flag.String("root", "", "")
	out := flag.String("out", "", "")
	flag.Parse()
	f, e := os.Open(filepath.Join(*root, "截图索引.csv"))
	fail(e)
	rows, e := csv.NewReader(f).ReadAll()
	fail(e)
	f.Close()
	c, e := evidence.Load()
	fail(e)
	report := map[string]any{"source_zip_sha256": c.ZipSHA, "real_screenshot_offline_replay": true, "real_game_tested": false, "count": len(rows) - 1}
	var found []map[string]any
	images := map[int]image.Image{}
	for _, r := range rows[1:] {
		id, e := strconv.Atoi(r[0])
		fail(e)
		p := filepath.Join(*root, filepath.FromSlash(r[2]))
		b, e := os.ReadFile(p)
		fail(e)
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != r[3] {
			fail(fmt.Errorf("image %d SHA mismatch", id))
		}
		f, e := os.Open(p)
		fail(e)
		im, _, e := image.Decode(f)
		f.Close()
		fail(e)
		images[id] = im
		var matches []string
		for _, v := range c.Matches(im) {
			matches = append(matches, v.Key)
			if v.Key == "advert" && id != 170 {
				fail(fmt.Errorf("advert false positive on %d", id))
			}
		}
		if len(matches) > 0 {
			found = append(found, map[string]any{"source_id": id, "matches": matches})
		}
	}
	for _, v := range c.Pages {
		if v.Derived != "" && v.Key != "selected_1" {
			continue
		}
		if !c.Match(images[v.SourceID], v.Key) {
			fail(fmt.Errorf("source image %d does not match %s", v.SourceID, v.Key))
		}
	}
	job, pe := c.FiveProfessions(images[144])
	report["profession_candidate"] = job
	report["five_profession_verified"] = pe == nil
	if pe != nil {
		report["profession_limitation"] = pe.Error()
	}
	report["matches"] = found
	report["passed"] = true
	b, e := json.MarshalIndent(report, "", "  ")
	fail(e)
	if *out != "" {
		fail(os.WriteFile(*out, append(b, '\n'), 0600))
	}
	fmt.Println(string(b))
}
