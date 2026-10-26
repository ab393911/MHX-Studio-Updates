package ldplayer

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDynamicList(t *testing.T) {
	all, e := ParseList2("10,ten,100,101,1,400,401\r\n6,six,60,61,1,300,301\r\n1,one,0,0,0,-1,-1\r\n")
	if e != nil {
		t.Fatal(e)
	}
	if len(all) != 3 || all[0].Index != 1 || all[1].Index != 6 || all[2].Index != 10 {
		t.Fatal(all)
	}
	if len(ParseDevices("List of devices attached\nemulator-7777 device model:x\n127.0.0.1:21999 device\nx offline\ny unauthorized")) != 2 {
		t.Fatal("devices parse")
	}
}
func TestRejectAmbiguousList(t *testing.T) {
	for _, s := range []string{"1,a,0,0,1,4,5\n1,b,0,0,1,6,7", "1,a,b,c,1,2,3", "garbage"} {
		if _, e := ParseList2(s); e == nil {
			t.Fatal(s)
		}
	}
}

type fakeRunner struct{ duplicate bool }

func (f fakeRunner) Run(_ context.Context, exe string, args ...string) ([]byte, error) {
	s := strings.Join(args, " ")
	if s == "list2" {
		return []byte("6,six,60,61,1,300,301\n10,ten,100,101,1,400,401"), nil
	}
	if strings.HasPrefix(s, "devices") {
		return []byte("List of devices attached\nodd-serial device\n127.0.0.1:21999 device\n"), nil
	}
	if strings.Contains(s, "android_id") {
		id := "abcdef0123456789"
		if strings.Contains(s, "--index 10") || strings.Contains(s, "127.0.0.1:21999") {
			id = "fedcba9876543210"
		}
		if f.duplicate {
			id = "abcdef0123456789"
		}
		return []byte(id), nil
	}
	return nil, fmt.Errorf("unexpected %s %s", exe, s)
}
func TestBindingUsesIdentityNotPortFormula(t *testing.T) {
	c := Client{Console: "console", ADB: "adb", Runner: fakeRunner{}}
	all, e := c.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if all[0].Index != 6 || all[0].Serial != "odd-serial" || all[1].Index != 10 || all[1].Serial != "127.0.0.1:21999" {
		t.Fatal(all)
	}
	c.Runner = fakeRunner{duplicate: true}
	if _, e = c.Discover(context.Background()); e == nil {
		t.Fatal("duplicate IDs bound")
	}
}
