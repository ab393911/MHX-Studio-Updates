// ldfixture is only a Windows CI test double for the documented LDPlayer CLI.
// It is never included in the user download or used to control real emulators.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

type vm struct {
	Index            int
	Running, Offline bool
	Serial           string
}

func die(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func main() {
	if len(os.Args) == 4 && os.Args[1] == "--serve-adb" {
		serveADB(os.Args[2], os.Args[3])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--hold" {
		for {
			time.Sleep(time.Minute)
		}
	}
	exe, e := os.Executable()
	die(e)
	root := filepath.Dir(exe)
	path := filepath.Join(root, "emulator-fixture.json")
	b, e := os.ReadFile(path)
	die(e)
	var all []vm
	die(json.Unmarshal(b, &all))
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "list2" {
		for _, d := range all {
			if d.Running {
				fmt.Printf("%d,雷电测试-%d,%d,%d,1,%d,%d,960,540,160\r\n", d.Index, d.Index, 100+d.Index, 200+d.Index, 7000+d.Index, 8000+d.Index)
			} else {
				fmt.Printf("%d,雷电测试-%d,0,0,0,-1,-1,960,540,160\r\n", d.Index, d.Index)
			}
		}
		return
	}
	if len(args) == 3 && args[0] == "launch" && args[1] == "--index" {
		n, e := strconv.Atoi(args[2])
		die(e)
		found := false
		for i := range all {
			if all[i].Index == n {
				all[i].Running = true
				found = true
			}
		}
		if !found {
			die(fmt.Errorf("unknown instance %d", n))
		}
		die(filesafe.JSON(path, all))
		f, e := os.OpenFile(filepath.Join(root, "launches.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		die(e)
		_, e = fmt.Fprintln(f, n)
		die(e)
		die(f.Close())
		return
	}
	if len(args) > 0 && args[0] == "devices" {
		fmt.Println("List of devices attached")
		for _, d := range all {
			if d.Running && !d.Offline {
				fmt.Printf("fixture-%d device model:fixture\n", d.Index)
			}
		}
		return
	}
	n := -1
	command := ""
	if len(args) >= 5 && args[0] == "adb" && args[1] == "--index" && args[3] == "--command" {
		n, e = strconv.Atoi(args[2])
		die(e)
		command = args[4]
	}
	if len(args) >= 3 && args[0] == "-s" {
		n, e = strconv.Atoi(strings.TrimPrefix(args[1], "fixture-"))
		die(e)
		command = strings.Join(args[2:], " ")
	}
	for _, d := range all {
		if d.Index != n {
			continue
		}
		if !d.Running || d.Offline {
			die(fmt.Errorf("device offline"))
		}
		if strings.Contains(command, "android_id") {
			fmt.Println("abcdef0123456789")
			return
		} // deliberate cloned Android ID
		if strings.Contains(command, "boot_id") {
			fmt.Printf("aaaaaaaa-bbbb-4ccc-8ddd-%012d\n", n)
			return
		}
	}
	die(fmt.Errorf("unsupported fixture command %v", args))
}
