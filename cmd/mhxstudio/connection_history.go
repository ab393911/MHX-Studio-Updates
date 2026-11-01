package main

import (
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"time"
)

type connectionFailure struct {
	Since  time.Time
	Reason string
}
type connectionHistory map[string]connectionFailure

// Only completed scans change history. An intermediate inventory isn't offline evidence.
func (h *connectionHistory) Observe(now time.Time, all []ldplayer.Instance) []string {
	if *h == nil {
		*h = connectionHistory{}
	}
	var events []string
	for _, d := range all {
		if d.Unmapped {
			continue
		}
		old, failed := (*h)[d.Key()]
		if d.Serial != "" && d.AndroidID != "" {
			if failed {
				events = append(events, fmt.Sprintf("编号%d ADB恢复%s；故障开始%s；原因%s；本轮身份已重新核验", d.Index, now.Format(time.RFC3339), old.Since.Format(time.RFC3339), old.Reason))
				delete(*h, d.Key())
			}
		} else if d.Running() {
			reason := d.ConnectionError
			if reason == "" {
				reason = d.Status()
			}
			if !failed {
				(*h)[d.Key()] = connectionFailure{now, reason}
				events = append(events, fmt.Sprintf("编号%d ADB未就绪开始%s：%s（不等同于明确offline）", d.Index, now.Format(time.RFC3339), reason))
			}
		}
	}
	return events
}
