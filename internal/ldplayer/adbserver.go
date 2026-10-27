package ldplayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ADBServer speaks the documented ADB smart-socket protocol to an already
// running local server. It does not install tools, restart servers or connect
// guessed emulator ports. Only discovery/identity shell requests are exposed.
type ADBServer struct{ Address string }
type ADBDevice struct{ Serial, State string }

func DefaultADBAddress() string {
	if s := os.Getenv("ADB_SERVER_SOCKET"); strings.HasPrefix(s, "tcp:") {
		s = strings.TrimPrefix(s, "tcp:")
		if !strings.Contains(s, ":") {
			s = "127.0.0.1:" + s
		}
		return s
	}
	host, port := os.Getenv("ANDROID_ADB_SERVER_ADDRESS"), os.Getenv("ANDROID_ADB_SERVER_PORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5037"
	}
	return net.JoinHostPort(host, port)
}
func (s ADBServer) dial(ctx context.Context) (net.Conn, error) {
	h, p, e := net.SplitHostPort(s.Address)
	if e != nil {
		return nil, fmt.Errorf("ADB 服务地址无效：%w", e)
	}
	port, e := strconv.Atoi(p)
	ip := net.ParseIP(h)
	if e != nil || port < 1 || port > 65535 || (h != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, errors.New("只允许连接本机 ADB 服务")
	}
	c, e := (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(ctx, "tcp", s.Address)
	if e != nil {
		return nil, e
	}
	deadline := time.Now().Add(4 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e = c.SetDeadline(deadline); e != nil {
		c.Close()
		return nil, e
	}
	return c, nil
}
func adbBlock(c io.Reader) ([]byte, error) {
	var header [4]byte
	if _, e := io.ReadFull(c, header[:]); e != nil {
		return nil, e
	}
	n, e := strconv.ParseUint(string(header[:]), 16, 16)
	if e != nil {
		return nil, errors.New("ADB 长度响应无效")
	}
	b := make([]byte, int(n))
	_, e = io.ReadFull(c, b)
	return b, e
}
func adbRequest(c net.Conn, request string) error {
	if len(request) > 65535 {
		return errors.New("ADB 请求过长")
	}
	if _, e := io.WriteString(c, fmt.Sprintf("%04x%s", len(request), request)); e != nil {
		return e
	}
	var status [4]byte
	if _, e := io.ReadFull(c, status[:]); e != nil {
		return e
	}
	if string(status[:]) == "OKAY" {
		return nil
	}
	if string(status[:]) == "FAIL" {
		b, e := adbBlock(c)
		if e != nil {
			return e
		}
		return fmt.Errorf("ADB 拒绝请求：%s", b)
	}
	return errors.New("ADB 服务响应无效")
}
func (s ADBServer) Devices(ctx context.Context) ([]ADBDevice, error) {
	c, e := s.dial(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if e = adbRequest(c, "host:devices-l"); e != nil {
		return nil, e
	}
	b, e := adbBlock(c)
	if e != nil {
		return nil, e
	}
	return parseADBDevices(string(b)), nil
}
func parseADBDevices(text string) []ADBDevice {
	seen := map[string]bool{}
	var out []ADBDevice
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || seen[f[0]] {
			continue
		}
		switch f[1] {
		case "device", "offline", "unauthorized", "no":
		default:
			continue
		}
		seen[f[0]] = true
		out = append(out, ADBDevice{f[0], f[1]})
		if len(out) >= 256 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Serial < out[j].Serial })
	return out
}
func (s ADBServer) Identity(ctx context.Context, serial, command string) (string, error) {
	if command != "settings get secure android_id" && command != "cat /proc/sys/kernel/random/boot_id" {
		return "", errors.New("只允许读取设备身份")
	}
	if serial == "" || len(serial) > 512 || strings.ContainsAny(serial, "\r\n\x00") {
		return "", errors.New("ADB 设备序列号无效")
	}
	c, e := s.dial(ctx)
	if e != nil {
		return "", e
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if e = adbRequest(c, "host:transport:"+serial); e != nil {
		return "", e
	}
	if e = adbRequest(c, "shell:"+command); e != nil {
		return "", e
	}
	b, e := io.ReadAll(io.LimitReader(c, 65537))
	if e != nil {
		return "", e
	}
	if len(b) > 65536 {
		return "", errors.New("ADB 身份响应过长")
	}
	return string(b), nil
}
