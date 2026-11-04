package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIssue2NumericVersionSequence(t *testing.T) {
	for _, c := range []struct {
		candidate, installed string
		want                 int
	}{
		{"3", "2", 1}, {"4", "3", 1}, {"3", "3", 0},
		{"2", "3", -1}, {"3", "4", -1}, {"10", "9", 1},
	} {
		candidate, installed := "3.5.0-preview.41-issue2."+c.candidate, "3.5.0-preview.41-issue2."+c.installed
		got, err := Compare(candidate, installed)
		if err != nil || got != c.want {
			t.Fatalf("%s vs %s = %d, %v; want %d", candidate, installed, got, err, c.want)
		}
	}
}

func TestIssue2HTTPSCheckRejectsSameAndOlderVersions(t *testing.T) {
	var packageRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/update.json" {
			packageRequests.Add(1)
			http.Error(w, "unexpected package request", 500)
			return
		}
		m := Manifest{Schema: 1, Version: r.URL.Query().Get("version"), Platform: "windows-amd64", URL: "https://example.invalid/MHXStudio.exe", SHA256: strings.Repeat("a", 64), Size: 2048}
		if err := json.NewEncoder(w).Encode(m); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	for _, c := range []struct {
		candidate, installed string
		want                 bool
	}{
		{"3", "2", true}, {"4", "3", true}, {"3", "3", false}, {"2", "3", false}, {"3", "4", false},
	} {
		candidate, installed := "3.5.0-preview.41-issue2."+c.candidate, "3.5.0-preview.41-issue2."+c.installed
		m, available, err := Check(context.Background(), server.Client(), server.URL+"/update.json?version="+url.QueryEscape(candidate), installed)
		if err != nil || available != c.want || m.Version != candidate {
			t.Fatalf("candidate %s installed %s: available=%v err=%v", candidate, installed, available, err)
		}
	}
	if packageRequests.Load() != 0 {
		t.Fatal("version check requested a package")
	}
}
