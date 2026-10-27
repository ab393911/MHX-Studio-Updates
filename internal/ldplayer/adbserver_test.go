package ldplayer

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testADBServer(t *testing.T, devices string) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	read := func(c net.Conn) (string, error) {
		var h [4]byte
		if _, e := io.ReadFull(c, h[:]); e != nil {
			return "", e
		}
		n, e := strconv.ParseUint(string(h[:]), 16, 16)
		if e != nil {
			return "", e
		}
		b := make([]byte, int(n))
		_, e = io.ReadFull(c, b)
		return string(b), e
	}
	write := func(c net.Conn, s string) {
		for i := 0; i < len(s); i += 3 {
			end := i + 3
			if end > len(s) {
				end = len(s)
			}
			_, _ = io.WriteString(c, s[i:end])
		}
	}
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				s, e := read(c)
				if e != nil {
					return
				}
				if s == "host:devices-l" {
					write(c, fmt.Sprintf("OKAY%04x%s", len(devices), devices))
					return
				}
				if !strings.HasPrefix(s, "host:transport:") {
					write(c, "FAIL0003bad")
					return
				}
				serial := strings.TrimPrefix(s, "host:transport:")
				write(c, "OKAY")
				s, e = read(c)
				if e != nil {
					return
				}
				switch s {
				case "shell:settings get secure android_id":
					write(c, "OKAYabcdef0123456789\n")
				case "shell:cat /proc/sys/kernel/random/boot_id":
					boot := "aaaaaaaa-bbbb-4ccc-8ddd-000000000001"
					if strings.Contains(serial, "21999") {
						boot = "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
					}
					write(c, "OKAY"+boot+"\n")
				default:
					write(c, "FAIL0003bad")
				}
			}()
		}
	}()
	return l.Addr().String()
}
func TestNativeADBProtocolHandlesFragmentedResponses(t *testing.T) {
	address := testADBServer(t, "odd-serial\tdevice model:LD\n127.0.0.1:21999\tdevice\nother\toffline\n")
	s := ADBServer{address}
	d, e := s.Devices(context.Background())
	if e != nil || len(d) != 3 {
		t.Fatal(d, e)
	}
	id, e := s.Identity(context.Background(), "odd-serial", "settings get secure android_id")
	if e != nil || idFrom(id) != "abcdef0123456789" {
		t.Fatal(id, e)
	}
	if _, e = s.Identity(context.Background(), "odd-serial", "input tap 1 1"); e == nil {
		t.Fatal("native discovery must not expose clicks")
	}
	if _, e = (ADBServer{"192.0.2.1:5037"}).Devices(context.Background()); e == nil {
		t.Fatal("remote server accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.Devices(ctx); e == nil {
		t.Fatal("cancel ignored")
	}
}
func TestFallbackFindsDevicesWithoutAnyExecutable(t *testing.T) {
	address := testADBServer(t, "odd-serial\tdevice\n127.0.0.1:21999\tdevice\nunknown\toffline\n")
	d := Detect(context.Background(), SearchPlan{Address: address}, nil, nil)
	if len(d.Instances) != 3 || d.Client.Console != "" {
		t.Fatal(d)
	}
	for _, v := range d.Instances {
		if v.Index != -1 || !v.Unmapped || v.Serial == "" {
			t.Fatal("guessed instance index", v)
		}
	}
	if d.Instances[0].AndroidID == "" || d.Instances[2].ADBState != "offline" {
		t.Fatal(d.Instances)
	}
	if e := d.Client.Verify(context.Background(), d.Instances[0]); e == nil {
		t.Fatal("unmapped device permitted control")
	}
	if !strings.Contains(Summary(d.Instances), "编号待匹配") {
		t.Fatal(Summary(d.Instances))
	}
}
func TestNativeADBMapsKnownIndicesByIdentityWithoutPortFormula(t *testing.T) {
	address := testADBServer(t, "odd-serial\tdevice\n127.0.0.1:21999\tdevice\n")
	c := Client{Console: "console", Server: address, Runner: bindingRunner{clones: true}}
	all, e := c.Discover(context.Background())
	if e != nil || all[0].Serial != "odd-serial" || all[1].Serial != "127.0.0.1:21999" {
		t.Fatal(all, e)
	}
	if e = c.Verify(context.Background(), all[0]); e == nil {
		t.Fatal("missing control executable must not start clicks")
	}
}

type fallbackRunner struct{}

func (fallbackRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	s := strings.Join(args, " ")
	if s == "list2" {
		return nil, fmt.Errorf("management executable failed")
	}
	if s == "devices -l" {
		return []byte("List of devices attached\nserial-unrelated-to-index device\n"), nil
	}
	if strings.Contains(s, "android_id") {
		return []byte("abcdef0123456789"), nil
	}
	if strings.Contains(s, "boot_id") {
		return []byte("aaaaaaaa-bbbb-4ccc-8ddd-000000000001"), nil
	}
	return nil, fmt.Errorf("unexpected command %s", s)
}
func TestBrokenManagerFallsBackToIndependentADBExecutable(t *testing.T) {
	p := SearchPlan{Installations: []Installation{{Console: "broken-console"}}, ADBs: []string{"independent-adb"}, Address: "127.0.0.1:1"}
	d := Detect(context.Background(), p, fallbackRunner{}, nil)
	if len(d.Instances) != 1 || d.Instances[0].Serial != "serial-unrelated-to-index" || !d.Instances[0].Unmapped || d.ValidatedDirectory != "" || d.ValidatedADB != "independent-adb" {
		t.Fatal(d)
	}
	found := false
	for _, c := range d.Checks {
		if strings.Contains(c.Result, "management executable failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("failure cause missing", d.Checks)
	}
}
func TestFailedSearchRetainsCheckedPathsAndServiceError(t *testing.T) {
	p := searchPlan("", "", "", nil, nil, "127.0.0.1:1")
	p.Installations = nil
	p.ADBs = nil // the checked D: hint must not depend on the test host's installations
	d := Detect(context.Background(), p, fallbackRunner{}, nil)
	foundPath, foundServer := false, false
	for _, c := range d.Checks {
		if strings.Contains(c.Path, `D:\LDPlayer14`) {
			foundPath = true
		}
		if c.Kind == "ADB本地服务" && c.Result != "" {
			foundServer = true
		}
	}
	if !foundPath || !foundServer || len(d.Instances) != 0 {
		t.Fatal(d)
	}
}

func TestSupplementalEmptyServerDoesNotDiscardVerifiedInstances(t *testing.T) {
	address := testADBServer(t, "")
	p := SearchPlan{Address: address, Installations: []Installation{{Console: "console", ADB: "adb"}}}
	d := Detect(context.Background(), p, bindingRunner{offline: true}, nil)
	if len(d.Instances) != 2 || d.Instances[0].Serial != "odd-serial" || d.Instances[1].ConnectionError == "" || d.Client.Server != "" {
		t.Fatal("supplemental scan lost working device", d)
	}
}
