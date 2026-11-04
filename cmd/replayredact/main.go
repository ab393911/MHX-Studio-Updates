// replayredact is a developer tool; it never runs in the MHX Studio GUI.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/fixtureprivacy"
)

type box struct{ X, Y, W, H int }

func rectangles(boxes []box) []image.Rectangle {
	var result []image.Rectangle
	for _, b := range boxes {
		if b.W <= 0 || b.H <= 0 {
			return nil
		}
		result = append(result, image.Rect(b.X, b.Y, b.X+b.W, b.Y+b.H))
	}
	return result
}
func main() {
	in := flag.String("in", "", "private input PNG")
	out := flag.String("out", "", "new local preview PNG; must not exist")
	policy := flag.String("policy", "", "local masks/protected JSON")
	auditPath := flag.String("audit", "", "new local audit JSON; must not exist")
	profilePath := flag.String("profile", "", "optional private rules for exact score comparison")
	flag.Parse()
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if *in == "" || *out == "" || *policy == "" || *auditPath == "" {
		fail(fmt.Errorf("in/out/policy/audit are required"))
	}
	raw, err := os.ReadFile(*in)
	fail(err)
	if len(raw) > 8<<20 {
		fail(fmt.Errorf("input exceeds 8MiB"))
	}
	config, err := os.ReadFile(*policy)
	fail(err)
	var p struct{ Masks, Protected []box }
	fail(json.Unmarshal(config, &p))
	protected := rectangles(p.Protected)
	rules := automation.NewProfile()
	if *profilePath != "" {
		profileRaw, err := os.ReadFile(*profilePath)
		fail(err)
		fail(json.Unmarshal(profileRaw, &rules))
		fail(rules.Validate())
		for _, page := range rules.Pages {
			for _, a := range page.Anchors {
				r := a.Rect
				protected = append(protected, image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H))
			}
		}
		for _, r := range rules.Buttons {
			protected = append(protected, image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H))
		}
	}
	unique := map[image.Rectangle]bool{}
	var regions []image.Rectangle
	for _, r := range protected {
		if !unique[r] {
			unique[r] = true
			regions = append(regions, r)
		}
	}
	preview, audit, err := fixtureprivacy.Redact(raw, rectangles(p.Masks), regions)
	fail(err)
	if *profilePath != "" {
		before, err := automation.Decode(raw)
		fail(err)
		after, err := automation.Decode(preview)
		fail(err)
		for name := range rules.Pages {
			a, err := json.Marshal(rules.Explain(before, name))
			fail(err)
			b, err := json.Marshal(rules.Explain(after, name))
			fail(err)
			if !bytes.Equal(a, b) {
				fail(fmt.Errorf("redaction altered recognition scores: %s", name))
			}
		}
		audit.RulePagesChecked = len(rules.Pages)
		audit.RecognitionScoresUnchanged = true
	}
	writeNew := func(path string, data []byte) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		fail(err)
		_, err = f.Write(data)
		closeErr := f.Close()
		fail(err)
		fail(closeErr)
	}
	writeNew(*out, preview)
	encoded, err := json.MarshalIndent(audit, "", "  ")
	fail(err)
	writeNew(*auditPath, append(encoded, '\n'))
	fmt.Printf("Local preview verified: %dx%d %s; %d protected regions unchanged\n", audit.Width, audit.Height, audit.Format, audit.ProtectedRegions)
}
