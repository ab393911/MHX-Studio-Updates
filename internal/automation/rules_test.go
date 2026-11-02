package automation

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestMultiFeatureRulesKeepLegacyStrictDefaultsAndExplicitThresholds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	p := NewProfile()
	var anchors []Anchor
	for i := 0; i < 3; i++ {
		a, _ := CaptureAnchor(img, Rect{10 + i*50, 10, 32, 16})
		anchors = append(anchors, a)
	}
	p.Pages["custom"] = Page{Anchors: anchors, Threshold: 10, Revision: 4, FeatureThresholds: []float64{10, 5, 10}}
	for y := 10; y < 26; y++ {
		for x := 60; x < 92; x++ {
			img.Set(x, y, color.Gray{6})
		}
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	score := p.Score(img, "custom")
	if score.Passed || score.RequiredMatches != 3 || score.MatchedFeatures != 2 || score.Anchors[1].MeanLimit != 5 || score.Revision != 4 {
		t.Fatal(score)
	}
	page := p.Pages["custom"]
	page.MinMatches = 2
	p.Pages["custom"] = page
	if !p.Score(img, "custom").Passed {
		t.Fatal("explicit 2/3 condition not respected")
	}
	page.MinMatches = 1
	p.Pages["custom"] = page
	if p.Validate() == nil || p.Match(img, "custom") {
		t.Fatal("single-feature condition accepted")
	}
	page.MinMatches = 2
	page.Anchors[1].Pixels = nil
	p.Pages["custom"] = page
	if p.Validate() == nil || p.Match(img, "custom") {
		t.Fatal("quorum bypassed corrupt anchor")
	}
}
func TestGenericRuleDetectionRejectsAmbiguityAndProvidesFailureScores(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	a, _ := CaptureAnchor(img, Rect{10, 10, 32, 16})
	p := NewProfile()
	p.Pages["custom_a"] = Page{Anchors: []Anchor{a, a}, Threshold: 10}
	p.Pages["custom_b"] = p.Pages["custom_a"]
	if _, scores, err := p.DetectRules(img, []string{"custom_a", "custom_b"}); err == nil || len(scores) != 2 {
		t.Fatal(err, scores)
	}
	if name, _, err := p.DetectRules(img, []string{"custom_a", "custom_a"}); err != nil || name != "custom_a" {
		t.Fatal(name, err)
	}
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			img.Set(x, y, color.Gray{100})
		}
	}
	err := p.Require(img, "custom_a")
	var f *RecognitionFailure
	if !errors.As(err, &f) || len(f.Scores) != 2 || f.Scores[0].Anchors[0].MeanAbsoluteError != 100 {
		t.Fatal(err)
	}
	s := p.Explain(img, "game")
	if s.Passed {
		t.Fatal(s)
	}
}
func TestFailedGameExplainRetainsBothActualSamples(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	a, _ := CaptureAnchor(img, Rect{906, 356, 36, 46})
	b, _ := CaptureAnchor(img, Rect{908, 411, 31, 44})
	p := NewProfile()
	p.Pages["game"] = Page{Anchors: []Anchor{a, b}, Threshold: 10}
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			img.Set(x, y, color.Gray{100})
		}
	}
	s := p.Explain(img, "game")
	if s.Passed || s.Error == "" || len(s.Anchors[0].CurrentPixels) != 512 || s.Anchors[1].BestOffsetDiagnostic == nil {
		t.Fatal(s)
	}
}
