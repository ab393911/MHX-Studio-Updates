package main

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

// CI-only protocol server. It cannot launch or control a VM and is not shipped.
func serveADB(state, ready string) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	die(e)
	die(filesafe.JSON(ready, map[string]string{"address": l.Addr().String()}))
	for {
		c, e := l.Accept()
		die(e)
		go serveADBConnection(c, state)
	}
}
func fixtureRequest(c net.Conn) (string, error) {
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
func fixtureResponse(c net.Conn, ok bool, payload string, framed bool) {
	status := "OKAY"
	if !ok {
		status = "FAIL"
		framed = true
	}
	if framed {
		_, _ = fmt.Fprintf(c, "%s%04x%s", status, len(payload), payload)
	} else {
		_, _ = io.WriteString(c, status+payload)
	}
}
func fixtureSerial(d vm) string {
	if d.Serial != "" {
		return d.Serial
	}
	return fmt.Sprintf("fixture-%d", d.Index)
}
func serveADBConnection(c net.Conn, state string) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(8 * time.Second))
	var all []vm
	if e := filesafe.ReadJSON(state, &all); e != nil {
		fixtureResponse(c, false, e.Error(), true)
		return
	}
	request, e := fixtureRequest(c)
	if e != nil {
		return
	}
	if request == "host:devices-l" {
		var b strings.Builder
		for _, d := range all {
			if d.Running {
				status := "device"
				if d.Offline {
					status = "offline"
				}
				fmt.Fprintf(&b, "%s\t%s model:ci_fixture\n", fixtureSerial(d), status)
			}
		}
		fixtureResponse(c, true, b.String(), true)
		return
	}
	if !strings.HasPrefix(request, "host:transport:") {
		fixtureResponse(c, false, "unsupported service", true)
		return
	}
	serial := strings.TrimPrefix(request, "host:transport:")
	for _, d := range all {
		if fixtureSerial(d) != serial || !d.Running || d.Offline {
			continue
		}
		fixtureResponse(c, true, "", false)
		request, e = fixtureRequest(c)
		if e != nil {
			return
		}
		switch request {
		case "shell:settings get secure android_id":
			fixtureResponse(c, true, "abcdef0123456789\n", false)
		case "shell:cat /proc/sys/kernel/random/boot_id":
			fixtureResponse(c, true, fmt.Sprintf("aaaaaaaa-bbbb-4ccc-8ddd-%012d\n", d.Index), false)
		default:
			fixtureResponse(c, false, "CI discovery server does not permit control", true)
		}
		return
	}
	fixtureResponse(c, false, "device offline or unknown", true)
}
