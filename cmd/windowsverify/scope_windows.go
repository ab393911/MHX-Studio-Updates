//go:build windows

package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

// Only the test runner trusts this ephemeral loopback certificate. Production
// updater HTTPS/TLS validation is unchanged. No release or channel is created.
func localGUIUpgrade(old, oldVersion, newExe, version, fixture, out string) map[string]any {
	fmt.Println("Preparing private HTTPS candidate for published baseline:", oldVersion)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	binary, e := os.ReadFile(newExe)
	fail(e)
	hash, e := update.Hash(newExe)
	fail(e)
	self, e := exec.CommandContext(ctx, newExe, "--self-test").Output()
	fail(e)
	var meta struct {
		Version string `json:"version"`
		Commit  string `json:"source_commit"`
	}
	fail(json.Unmarshal(self, &meta))
	if meta.Version != version || len(meta.Commit) != 40 {
		fail(fmt.Errorf("candidate metadata mismatch %s", self))
	}
	var manifest update.Manifest
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			w.Header().Set("Content-Type", "application/json")
			fail(json.NewEncoder(w).Encode(manifest))
		case "/MHXStudio.exe":
			w.Header().Set("Content-Length", fmt.Sprint(len(binary)))
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manifest = update.Manifest{Schema: 1, Version: version, Platform: "windows-amd64", URL: server.URL + "/MHXStudio.exe", SHA256: hash, Size: int64(len(binary)), SourceCommit: meta.Commit, WindowsTested: false}
	cert := server.Certificate()
	thumb := sha1.Sum(cert.Raw)
	thumbText := hex.EncodeToString(thumb[:])
	temp, e := os.MkdirTemp("", "MHX CI TLS ")
	fail(e)
	defer os.RemoveAll(temp)
	certPath := filepath.Join(temp, "loopback.cer")
	fail(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600))
	if b, e := exec.CommandContext(ctx, "certutil", "-user", "-addstore", "Root", certPath).CombinedOutput(); e != nil {
		fail(fmt.Errorf("test TLS trust import: %w %s", e, b))
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if b, e := exec.CommandContext(cleanup, "certutil", "-user", "-delstore", "Root", thumbText).CombinedOutput(); e != nil {
			fail(fmt.Errorf("test TLS trust cleanup: %w %s", e, b))
		}
	}()
	// Go's Windows verifier and the published EXE use the regular system chain.
	fmt.Println("Private HTTPS trust ready:", oldVersion)
	liveUpdate(old, oldVersion, server.URL+"/update.json", version, out, fixture, true)
	var report map[string]any
	fail(filesafe.ReadJSON(out, &report))
	report["candidate_transport"] = "ephemeral_loopback_https_trusted_certificate"
	report["candidate_published"] = false
	report["production_channel_changed"] = false
	fail(filesafe.JSON(out, report))
	return report
}

func seedPublished21Plan(h uintptr) (string, string) {
	gameIdle(h)
	click(h, 106)
	title := "MHX Studio 工作台 · 五开计划与订单"
	fail(waitWindow(title))
	p := findWindow(title)
	for i := 0; i < 5; i++ {
		user.NewProc("SendMessageW").Call(control(p, 510), 0x185, 1, uintptr(i))
	}
	writeText(control(p, 511), "一梦诛仙,另一服")
	click(p, 503)
	until(h, "published preview.21 plan not created", func() bool { return strings.Contains(readText(control(p, 515)), "角色1小组") })
	writeText(control(p, 513), "ci-scope-legacy-buyer")
	writeText(control(p, 514), "7万")
	click(p, 507)
	until(h, "published preview.21 order not created", func() bool { return strings.Contains(readText(control(p, 515)), "总额70000 确认已交0") })
	text := readText(control(p, 515))
	match := regexp.MustCompile(`计划 ([a-f0-9]{32})`).FindStringSubmatch(text)
	order := regexp.MustCompile(`订单([a-f0-9]{32}) 买家ci-scope-legacy-buyer`).FindStringSubmatch(text)
	if len(match) != 2 || len(order) != 2 {
		fail(fmt.Errorf("legacy plan/order IDs missing: %s", text))
	}
	closeTitle(title)
	gameIdle(h)
	return match[1], order[1]
}
func verifyMigrated21UI(h uintptr, dataRoot, planID, orderID string) {
	// Opening the real workbench must display only active tasks. All states remain
	// pending: creating/upgrading a plan is never recorded as actual game success.
	gameIdle(h)
	click(h, 106)
	title := "MHX Studio 工作台 · 五开计划与订单"
	fail(waitWindow(title))
	p := findWindow(title)
	until(h, "migrated order/plan missing", func() bool {
		return strings.Contains(readText(control(p, 515)), planID) && strings.Contains(readText(control(p, 515)), orderID)
	})
	text := readText(control(p, 515))
	for _, removed := range []string{"锁灵塔", "日课", "群英榜"} {
		if strings.Contains(text, removed) {
			fail(fmt.Errorf("removed current board entry %s", removed))
		}
	}
	s, e := studio.Open(dataRoot)
	fail(e)
	plan, e := s.Plan(planID)
	fail(e)
	if !slices.Equal(plan.Modules, studio.EnabledModules()) || len(plan.PhaseLayout) != 11 || plan.HistoricalBatches != 1 {
		fail(fmt.Errorf("legacy physical layout lost %+v", plan))
	}
	if len(plan.Phases()) != 80 || plan.Phases()[7].Number != 10 || plan.Phases()[7].Module != "battle_pass" || plan.Phases()[8].Number != 11 {
		fail(fmt.Errorf("migrated phase identities invalid"))
	}
	phase, e := s.Next(planID, time.Now())
	fail(e)
	for _, task := range phase.Tasks {
		if task.State != studio.Pending {
			fail(fmt.Errorf("upgrade invented task success"))
		}
	}
	o, e := s.Order(orderID)
	fail(e)
	if o.Total != 70000 || o.Confirmed != 0 || o.Held != 0 || o.DeliveryEnabled {
		fail(fmt.Errorf("legacy record-only order changed %+v", o))
	}
	fail(s.Close())
	closeTitle(title)
	gameIdle(h)
}
