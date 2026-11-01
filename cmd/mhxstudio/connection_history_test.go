package main

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"strings"
	"testing"
	"time"
)

func TestCompletedScanFailureAndRecoveryKeepOriginalStart(t *testing.T) {
	var h connectionHistory
	now := time.Now()
	d := ldplayer.Instance{Index: 4, PID: 400, Android: true, ConnectionError: "device offline"}
	if len(h.Observe(now, []ldplayer.Instance{d})) != 1 {
		t.Fatal("missing failure")
	}
	if len(h.Observe(now.Add(time.Second), []ldplayer.Instance{d})) != 0 {
		t.Fatal("duplicate failure")
	}
	d.Serial = "new-serial"
	d.AndroidID = "verified"
	events := h.Observe(now.Add(12*time.Second), []ldplayer.Instance{d})
	if len(events) != 1 || !strings.Contains(events[0], now.Format(time.RFC3339)) || len(h) != 0 {
		t.Fatal(events, h)
	}
}
