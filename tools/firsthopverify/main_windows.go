//go:build windows && amd64

// Disposable CI diagnostic only. Hardware breakpoints observe an immutable
// published helper; no executable bytes, production data or registry are patched.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

const publishedSHA = "e58eaef41b4e37361f79cc500872bc89f1e7ebe37d810785740b294eb397daa2"

var k = syscall.NewLazyDLL("kernel32.dll")
var u = syscall.NewLazyDLL("user32.dll")

func check(e error) {
	if e != nil {
		panic(e)
	}
}
func win(name string, args ...uintptr) uintptr {
	r, _, e := k.NewProc(name).Call(args...)
	if r == 0 {
		panic(fmt.Errorf("%s: %w", name, e))
	}
	return r
}
func closeH(h uintptr) {
	if h != 0 {
		syscall.CloseHandle(syscall.Handle(h))
	}
}
func utf(s string) *uint16 { p, e := syscall.UTF16PtrFromString(s); check(e); return p }
func readMem(process, address uintptr, n int) []byte {
	b := make([]byte, n)
	var got uintptr
	win("ReadProcessMemory", process, address, uintptr(unsafe.Pointer(&b[0])), uintptr(n), uintptr(unsafe.Pointer(&got)))
	if got != uintptr(n) {
		panic("short remote read")
	}
	return b
}
func remoteString(process, address uintptr) string {
	var chars []uint16
	for i := 0; i < 4096; i++ {
		v := binary.LittleEndian.Uint16(readMem(process, address+uintptr(i*2), 2))
		if v == 0 {
			return syscall.UTF16ToString(chars)
		}
		chars = append(chars, v)
	}
	panic("remote path too long")
}
func samePath(a, b string) bool {
	canonical := func(path string) string {
		path = strings.TrimPrefix(path, `\\?\`)
		buf := make([]uint16, 4096)
		n, _, _ := k.NewProc("GetLongPathNameW").Call(uintptr(unsafe.Pointer(utf(path))), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 && n < uintptr(len(buf)) {
			path = syscall.UTF16ToString(buf[:n])
		}
		return filepath.Clean(path)
	}
	return strings.EqualFold(canonical(a), canonical(b))
}

// AMD64 CONTEXT is 16-byte aligned. Offsets are from the Windows AMD64 ABI.
type cpu struct {
	storage [1248]byte
	p       unsafe.Pointer
}

func threadCPU(tid uint32) (uintptr, *cpu) {
	h := win("OpenThread", 0x58, 0, uintptr(tid))
	c := &cpu{}
	base := unsafe.Pointer(&c.storage[0])
	c.p = unsafe.Add(base, (-uintptr(base))&15)
	binary.LittleEndian.PutUint32(unsafe.Slice((*byte)(unsafe.Add(c.p, 48)), 4), 0x00100013)
	win("GetThreadContext", h, uintptr(c.p))
	runtime.KeepAlive(c)
	return h, c
}
func (c *cpu) get(off uintptr) uint64 {
	return binary.LittleEndian.Uint64(unsafe.Slice((*byte)(unsafe.Add(c.p, off)), 8))
}
func (c *cpu) set(off uintptr, v uint64) {
	binary.LittleEndian.PutUint64(unsafe.Slice((*byte)(unsafe.Add(c.p, off)), 8), v)
}
func (c *cpu) resume() {
	p := unsafe.Slice((*byte)(unsafe.Add(c.p, 68)), 4)
	binary.LittleEndian.PutUint32(p, binary.LittleEndian.Uint32(p)|0x10000)
}
func hardware(tid uint32, address uintptr) {
	h, c := threadCPU(tid)
	defer closeH(h)
	c.set(72, uint64(address))
	c.set(104, 0)
	c.set(112, 1)
	win("SetThreadContext", h, uintptr(c.p))
	runtime.KeepAlive(c)
}
func localAPI() (string, uintptr, uintptr) {
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("NtSetInformationFile")
	check(proc.Find())
	var module uintptr
	win("GetModuleHandleExW", 6, proc.Addr(), uintptr(unsafe.Pointer(&module)))
	name := make([]uint16, 1024)
	n := win("GetModuleFileNameW", module, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	return filepath.Base(syscall.UTF16ToString(name[:n])), proc.Addr() - module, module
}
func moduleName(h uintptr) string {
	if h == 0 {
		return ""
	}
	b := make([]uint16, 2048)
	r, _, _ := k.NewProc("GetFinalPathNameByHandleW").Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
	if r == 0 || r >= uintptr(len(b)) {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(b[:r]))
}

func remoteHandlePath(process, remote uintptr) string {
	var local uintptr
	current := win("GetCurrentProcess")
	win("DuplicateHandle", process, remote, current, uintptr(unsafe.Pointer(&local)), 0, 0, 2)
	defer closeH(local)
	buf := make([]uint16, 2048)
	n := win("GetFinalPathNameByHandleW", local, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n >= uintptr(len(buf)) {
		panic("remote file path too long")
	}
	return syscall.UTF16ToString(buf[:n])
}

// Known system DLLs normally share their boot-time mapping. Verify the remote
// PE export-name and syscall bytes before using that mapping when the loader
// supplies no DLL file handle. This is read-only observation, not injection.
func sameSystemModule(process, base, offset uintptr, name string) bool {
	read := func(address uintptr, n int) ([]byte, bool) {
		b := make([]byte, n)
		var got uintptr
		r, _, _ := k.NewProc("ReadProcessMemory").Call(process, address, uintptr(unsafe.Pointer(&b[0])), uintptr(n), uintptr(unsafe.Pointer(&got)))
		return b, r != 0 && got == uintptr(n)
	}
	dos, ok := read(base, 64)
	if !ok || string(dos[:2]) != "MZ" {
		return false
	}
	pe := uintptr(binary.LittleEndian.Uint32(dos[60:64]))
	hdr, ok := read(base+pe, 144)
	if !ok || string(hdr[:4]) != "PE\x00\x00" || binary.LittleEndian.Uint16(hdr[24:26]) != 0x20b {
		return false
	}
	exportRVA := binary.LittleEndian.Uint32(hdr[136:140])
	ex, ok := read(base+uintptr(exportRVA), 40)
	if !ok {
		return false
	}
	nameRVA := binary.LittleEndian.Uint32(ex[12:16])
	var remote []byte
	for i := 0; i < 128; i++ {
		b, ok := read(base+uintptr(nameRVA)+uintptr(i), 1)
		if !ok {
			return false
		}
		if b[0] == 0 {
			break
		}
		remote = append(remote, b[0])
	}
	if !strings.EqualFold(string(remote), name) {
		return false
	}
	code, ok := read(base+offset, 16)
	if !ok {
		return false
	}
	local := make([]byte, 16)
	var got uintptr
	current := win("GetCurrentProcess")
	okLocal, _, _ := k.NewProc("ReadProcessMemory").Call(current, base+offset, uintptr(unsafe.Pointer(&local[0])), 16, uintptr(unsafe.Pointer(&got)))
	return okLocal != 0 && got == 16 && bytes.Equal(code, local)
}

// Stop the REAL preview40 helper at the return of its successful first rename.
// Hardware breakpoints are thread-local; the helper file and code bytes stay intact.
func killAfterBackup(helper, planPath string, p update.Plan, guard func(int)) *exec.Cmd {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	module, offset, knownBase := localAPI()
	cmd := exec.Command(helper, "--apply-update", planPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 2} // DEBUG_ONLY_THIS_PROCESS
	check(cmd.Start())
	process := win("OpenProcess", 0x1051, 0, uintptr(cmd.Process.Pid))
	defer closeH(process)
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	guard(cmd.Process.Pid)
	threads := map[uint32]bool{}
	var entry, returnPC uintptr
	var watched uint32
	killed := false
	end := time.Now().Add(60 * time.Second)
	for time.Now().Before(end) {
		var event [176]byte // DEBUG_EVENT, 16-byte header + 160-byte union
		ok, _, e := k.NewProc("WaitForDebugEvent").Call(uintptr(unsafe.Pointer(&event[0])), 500)
		if ok == 0 {
			if e == syscall.Errno(121) {
				continue
			}
			panic(fmt.Errorf("WaitForDebugEvent: %w", e))
		}
		code := binary.LittleEndian.Uint32(event[0:4])
		pid := binary.LittleEndian.Uint32(event[4:8])
		tid := binary.LittleEndian.Uint32(event[8:12])
		d := event[16:]
		status := uintptr(0x00010002) // DBG_CONTINUE
		switch code {
		case 3: // CREATE_PROCESS_DEBUG_EVENT
			closeH(uintptr(binary.LittleEndian.Uint64(d[0:8])))
			closeH(uintptr(binary.LittleEndian.Uint64(d[8:16])))
			closeH(uintptr(binary.LittleEndian.Uint64(d[16:24])))
			threads[tid] = true
			if entry == 0 && sameSystemModule(process, knownBase, offset, module) {
				entry = knownBase + offset
				fmt.Println("debugger: verified remote system syscall mapping", module)
			}
			if entry != 0 {
				hardware(tid, entry)
			}
		case 2:
			closeH(uintptr(binary.LittleEndian.Uint64(d[0:8])))
			threads[tid] = true
			if entry != 0 {
				hardware(tid, entry)
			}
		case 4:
			delete(threads, tid)
		case 6: // LOAD_DLL_DEBUG_EVENT
			file := uintptr(binary.LittleEndian.Uint64(d[0:8]))
			name := moduleName(file)
			closeH(file)
			if strings.EqualFold(name, module) {
				fmt.Println("debugger: observing original helper API module", name)
				entry = uintptr(binary.LittleEndian.Uint64(d[8:16])) + offset
				for thread := range threads {
					hardware(thread, entry)
				}
			}
		case 1:
			exception := binary.LittleEndian.Uint32(d[0:4])
			if exception == 0x80000004 && entry != 0 {
				h, c := threadCPU(tid)
				rip := uintptr(c.get(248))
				if watched == tid && rip == returnPC {
					if uint32(c.get(120)) != 0 {
						closeH(h)
						panic("observed rename failed")
					}
					closeH(h)
					fmt.Println("debugger: original helper native rename returned STATUS_SUCCESS; terminating before caller resumes")
					win("TerminateProcess", process, 77)
					killed = true
				} else if rip == entry {
					class := binary.LittleEndian.Uint32(readMem(process, uintptr(c.get(152))+40, 4))
					// Native rename classes; ignore other file-information operations.
					if class == 10 || class == 56 || class == 65 || class == 66 {
						from := remoteHandlePath(process, uintptr(c.get(128)))
						if samePath(from, p.Target) {
							fmt.Println("debugger: original helper entered native rename of target EXE")
							returnPC = uintptr(binary.LittleEndian.Uint64(readMem(process, uintptr(c.get(152)), 8)))
							watched = tid
							c.set(72, uint64(returnPC))
							c.set(104, 0)
							c.set(112, 1)
						}
					}
					c.resume()
					win("SetThreadContext", h, uintptr(c.p))
					runtime.KeepAlive(c)
					closeH(h)
				} else {
					closeH(h)
					status = 0x80010001
				}
			} else if exception != 0x80000003 {
				status = 0x80010001
			}
		case 5:
			win("ContinueDebugEvent", uintptr(pid), uintptr(tid), status)
			err := cmd.Wait()
			if !killed || err == nil || cmd.ProcessState.ExitCode() != 77 {
				var state update.Result
				_ = filesafe.ReadJSON(filepath.Join(p.Dir(), "result.json"), &state)
				panic(fmt.Errorf("wrong exit, killed=%v: %v; transaction=%+v", killed, err, state))
			}
			return cmd
		}
		win("ContinueDebugEvent", uintptr(pid), uintptr(tid), status)
	}
	panic(fmt.Sprintf("bounded legacy probe did not reach rename return: API=%x thread=%d", entry, watched))
}

func supervise(planPath, helper string, pid int, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	handle := win("OpenProcess", 0x00100000, 0, uintptr(pid))
	defer closeH(handle)
	check(filesafe.Write(out+".ready", []byte("watching original helper"), 0600))
	for {
		r, _, e := k.NewProc("WaitForSingleObject").Call(handle, 100)
		if r == 0 {
			break
		}
		if r != 258 {
			panic(e)
		}
		if ctx.Err() != nil {
			panic(ctx.Err())
		}
	}
	var p update.Plan
	check(filesafe.ReadJSON(planPath, &p))
	check(update.Verify(helper, p.OldSHA))
	check(update.Verify(p.Backup, p.OldSHA))
	if _, err := os.Stat(p.Target); !os.IsNotExist(err) {
		panic("supervisor refuses to overwrite existing target")
	}
	cmd := exec.CommandContext(ctx, helper, "--apply-update", planPath)
	check(cmd.Run())
	check(update.Verify(p.Target, p.OldSHA))
	check(filesafe.JSON(out, map[string]any{"restored": true, "preinstalled_before_termination": true, "user_action_after_termination": false, "deployable_via_preview40_update": false}))
}
func waitGUI(target string) uint32 {
	end := time.Now().Add(25 * time.Second)
	for time.Now().Before(end) {
		h, _, _ := u.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(utf("知夏 · MHX Studio 3.5.0-preview.40"))))
		if h != 0 {
			var pid uint32
			u.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
			ph := win("OpenProcess", 0x1000, 0, uintptr(pid))
			buf := make([]uint16, 2048)
			n := uint32(len(buf))
			ok, _, _ := k.NewProc("QueryFullProcessImageNameW").Call(ph, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
			closeH(ph)
			if ok != 0 && samePath(syscall.UTF16ToString(buf[:n]), target) {
				visible, _, _ := u.NewProc("IsWindowVisible").Call(h)
				if visible != 0 {
					u.NewProc("PostMessageW").Call(h, 0x10, 0, 0)
					ph := win("OpenProcess", 0x00100000, 0, uintptr(pid))
					defer closeH(ph)
					wait, _, _ := k.NewProc("WaitForSingleObject").Call(ph, 15000)
					if wait != 0 {
						panic("restored GUI did not close cleanly")
					}
					return pid
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("restored published preview40 GUI did not become visible")
}
func runCase(old string, withGuard bool) map[string]any {
	fmt.Println("Actual preview40 helper case; preinstalled guard:", withGuard)
	root, e := os.MkdirTemp("", "MHX-real-firsthop-")
	check(e)
	defer os.RemoveAll(root)
	appdata := filepath.Join(root, "AppData")
	check(os.Setenv("LOCALAPPDATA", appdata))
	data := filepath.Join(appdata, "MHXStudioNext")
	check(os.MkdirAll(data, 0700))
	config := []byte("{\"retries\":7,\"future\":{\"keep\":true}}\n")
	check(os.WriteFile(filepath.Join(data, "config.json"), config, 0600))
	store, e := studio.Open(data)
	check(e)
	order, e := store.CreateOrder("CI-sentinel", 30000)
	check(e)
	check(store.Close())
	target := filepath.Join(root, "Program", "MHXStudio.exe")
	stage := filepath.Join(root, "Program", ".mhx-updates", "tx-real", "package.exe")
	check(os.MkdirAll(filepath.Dir(stage), 0700))
	check(filesafe.CopyNew(old, target))
	check(filesafe.CopyNew(old, stage))
	p, e := update.NewPlan(target, stage, "3.5.0-preview.41-issue2.1", publishedSHA)
	check(e)
	parent := exec.Command(os.Args[0], "--parent-exit")
	check(parent.Run())
	p.ParentPID = parent.Process.Pid
	p, e = update.WithConfig(p, data)
	check(e)
	helper := filepath.Join(p.Dir(), "updater.exe")
	check(filesafe.CopyNew(old, helper))
	check(update.Verify(helper, publishedSHA))
	var guard *exec.Cmd
	guardResult := filepath.Join(root, "supervisor.json")
	killAfterBackup(helper, filepath.Join(p.Dir(), "plan.json"), p, func(pid int) {
		if withGuard {
			self, e := os.Executable()
			check(e)
			guard = exec.Command(self, "--supervise", filepath.Join(p.Dir(), "plan.json"), "--helper", helper, "--pid", fmt.Sprint(pid), "--out", guardResult)
			check(guard.Start())
			end := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(guardResult + ".ready"); err == nil {
					break
				}
				if time.Now().After(end) {
					panic("supervisor was not ready before debuggee resumed")
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	check(update.Verify(helper, publishedSHA))
	check(update.Verify(p.Backup, publishedSHA))
	result := map[string]any{"actual_published_helper": true, "published_helper_sha256": publishedSHA, "helper_file_unchanged": true, "termination_exit_code": 77, "fault_point": "successful old EXE -> retained backup, before Go caller resumes", "preinstalled_supervisor": withGuard, "production_user_data_touched": false}
	if withGuard {
		check(guard.Wait())
		var observed map[string]any
		check(filesafe.ReadJSON(guardResult, &observed))
		result["supervisor"] = observed
		result["restored_gui_pid"] = waitGUI(target)
		result["automatic_recovery_with_preinstalled_guard"] = true
	} else {
		// Bounded observation with no recovery invocation. No supervisor/boot hook was installed.
		time.Sleep(3 * time.Second)
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			panic("unexpected unattended recovery")
		}
		result["automatic_recovery_without_prerequisite"] = false
		result["target_missing_after_observation"] = true
		// Recovery below is cleanup/proof of recoverability, never counted as automatic.
		check(exec.Command(helper, "--apply-update", filepath.Join(p.Dir(), "plan.json")).Run())
		result["restored_gui_pid"] = waitGUI(target)
		result["explicit_test_cleanup_restarted_helper"] = true
	}
	check(update.Verify(target, publishedSHA))
	got, e := os.ReadFile(filepath.Join(data, "config.json"))
	check(e)
	if !bytes.Equal(got, config) {
		panic("config changed")
	}
	store, e = studio.Open(data)
	check(e)
	kept, e := store.Order(order.ID)
	check(e)
	if kept.Total != 30000 {
		panic("order changed")
	}
	check(store.Close())
	result["old_exe_config_database_restorable"] = true
	return result
}
func main() {
	old := flag.String("old", "", "")
	out := flag.String("out", "firsthop-result.json", "")
	plan := flag.String("supervise", "", "")
	helper := flag.String("helper", "", "")
	pid := flag.Int("pid", 0, "")
	parentExit := flag.Bool("parent-exit", false, "")
	flag.Parse()
	if *parentExit {
		return
	}
	watchdog := time.AfterFunc(150*time.Second, func() {
		_ = filesafe.JSON(*out, map[string]any{"probe_completed": false, "error": "first-hop probe exceeded 150 seconds; no release"})
		os.Exit(2)
	})
	defer watchdog.Stop()
	defer func() {
		if r := recover(); r != nil {
			_ = filesafe.JSON(*out, map[string]any{"probe_completed": false, "error": fmt.Sprint(r)})
			fmt.Fprintln(os.Stderr, r)
			os.Exit(1)
		}
	}()
	if *plan != "" {
		supervise(*plan, *helper, *pid, *out)
		return
	}
	check(update.Verify(*old, publishedSHA))
	result := map[string]any{"source_binary": "actual published preview40", "probe_completed": false, "publication_ready": false}
	result["without_supervisor"] = runCase(*old, false)
	result["with_preinstalled_supervisor"] = runCase(*old, true)
	result["probe_completed"] = true
	result["first_hop_solved_under_existing_constraints"] = false
	result["reason"] = "preview40 cannot install or start the prerequisite supervisor before its first destructive rename; candidate code runs too late"
	check(filesafe.JSON(*out, result))
	check(json.NewEncoder(os.Stdout).Encode(result))
}
