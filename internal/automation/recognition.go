package automation

import (
	"fmt"
	"image"
	"sort"
)

// RecognitionFailure carries reproducible scores through existing wrapped errors.
// It changes reporting, never task transitions or retry/click policy.
type RecognitionFailure struct {
	Reason string
	Scores []PageScore
}

func (e *RecognitionFailure) Error() string { return e.Reason }
func (p Profile) failure(img image.Image, reason string) error {
	keys := make([]string, 0, len(p.Pages))
	for k := range p.Pages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	e := &RecognitionFailure{Reason: reason}
	for _, k := range keys {
		e.Scores = append(e.Scores, p.Score(img, k))
	}
	return e
}

// DetectRules is the task-independent interface for explicitly selected page IDs.
// Login keeps its established role/selected-row conflict rules in Recognize.
func (p Profile) DetectRules(img image.Image, ids []string) (string, []PageScore, error) {
	if err := p.Validate(); err != nil {
		return "", nil, err
	}
	if len(ids) == 0 {
		return "", nil, fmt.Errorf("no page rules requested")
	}
	seen := map[string]bool{}
	var matches []string
	var scores []PageScore
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		s := p.Score(img, id)
		scores = append(scores, s)
		if s.Passed {
			matches = append(matches, id)
		}
	}
	if len(matches) != 1 {
		return "", scores, &RecognitionFailure{Reason: fmt.Sprintf("页面未唯一确认：匹配%d项；未点击", len(matches)), Scores: scores}
	}
	return matches[0], scores, nil
}
