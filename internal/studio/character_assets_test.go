package studio

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCharacterAssetsReplacePerRoleAcrossAllFourServers(t *testing.T) {
	s := store(t)
	now := time.Now()
	servers := []string{"一区", "二区", "三区", "四区"}
	ids := []int{1, 2, 6}
	put := func(i, slot int, server string, silver, merit int64, at time.Time) {
		t.Helper()
		if e := s.RecordCharacterAsset(CharacterAsset{Instance: i, Slot: slot, Server: server, Silver: silver, Merit: merit, Evidence: evidence("asset"), ObservedAt: at}); e != nil {
			t.Fatal(e)
		}
	}
	put(1, 1, servers[0], 327000, 345, now)
	put(2, 1, servers[0], 9999, 900, now)
	put(6, 1, servers[0], 9999, 900, now)
	put(1, 2, servers[0], 50000, 90, now)
	put(1, 1, servers[1], 50000, 200, now)
	put(1, 1, servers[0], 327000, 345, now) // exact repeat never accumulates
	sum, e := s.ServerAssetSummary(servers, ids, now, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	if sum[0].Available != 30000 || sum[0].Scanned != 4 || sum[0].BelowMinimum != 3 || sum[0].Missing != 11 || sum[1].Available != 20000 || sum[2].Missing != 15 || sum[3].Available != 0 {
		t.Fatal(sum)
	}
	put(1, 1, servers[0], 20000, 100, now.Add(time.Millisecond))
	sum, e = s.ServerAssetSummary(servers, ids, now, time.Hour)
	if e != nil || sum[0].Available != 10000 {
		t.Fatal(sum, e)
	}
	if e = s.RecordCharacterAsset(CharacterAsset{Instance: 1, Slot: 1, Server: servers[0], Silver: 999999, Merit: 9999, Evidence: evidence("old"), ObservedAt: now}); e == nil {
		t.Fatal("old sample overwrote new")
	}
	sum, e = s.ServerAssetSummary(servers, ids, now.Add(2*time.Hour), time.Hour)
	if e != nil || sum[0].Available != 0 || sum[0].Stale != 4 {
		t.Fatal(sum, e)
	}
	// Inventory filtering excludes deleted/foreign instances without pooling them.
	sum, e = s.ServerAssetSummary(servers, []int{2, 6}, now, time.Hour)
	if e != nil || sum[0].Available != 0 || sum[0].Scanned != 2 || sum[0].Missing != 8 {
		t.Fatal(sum, e)
	}
}
func TestCharacterAssetsPersistAndBadInputsDoNotCreateSamples(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	a := CharacterAsset{Instance: 24, Slot: 5, Server: "四区", Silver: 50000, Merit: 200, Evidence: evidence("sample"), ObservedAt: now}
	if e = s.RecordCharacterAsset(a); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	sum, e := s.ServerAssetSummary([]string{"四区"}, []int{24}, now, time.Hour)
	if e != nil || sum[0].Available != 20000 || sum[0].Missing != 4 {
		t.Fatal(sum, e)
	}
	for _, mode := range []string{"slot", "time", "hash", "negative", "empty"} {
		b := a
		switch mode {
		case "slot":
			b.Slot = 6
		case "time":
			b.ObservedAt = now.Add(-time.Hour)
		case "hash":
			b.Evidence = "bad"
		case "negative":
			b.Silver = -1
		case "empty":
			b.Server = ""
		}
		if e = s.RecordCharacterAsset(b); e == nil {
			t.Fatal(mode)
		}
	}
	if _, e = os.Stat(filepath.Join(dir, FileName)); e != nil {
		t.Fatal(e)
	}
}
