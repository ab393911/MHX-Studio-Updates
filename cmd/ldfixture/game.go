package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

// game-fixture.json and PNGs are synthetic CI data, never real game templates.
type gameState struct {
	Package, Activity, Foreground, Page string
	Role                                int
	Running, Stall, WrongRole           bool
	Starts, Taps                        int
}

func gameCommand(root, command string, index int) bool {
	path := filepath.Join(root, fmt.Sprintf("game-fixture-%d.json", index))
	if _, e := os.Stat(path); os.IsNotExist(e) {
		path = filepath.Join(root, "game-fixture.json")
	}
	var g gameState
	if filesafe.ReadJSON(path, &g) != nil {
		return false
	}
	if strings.Contains(command, "query-activities") {
		fmt.Printf("com.android.launcher/.Launcher\n%s/%s\ncom.example.other/.Main\n", g.Package, g.Activity)
		return true
	}
	if strings.Contains(command, "dumpsys activity") || strings.Contains(command, "dumpsys window") {
		fmt.Printf("mResumedActivity: ActivityRecord{abc u0 %s/.Main t1}\n", g.Foreground)
		return true
	}
	if strings.Contains(command, "pidof") {
		if !g.Running {
			os.Exit(1)
		}
		fmt.Println("12345")
		return true
	}
	if strings.Contains(command, "am start") {
		if !strings.Contains(command, g.Package+"/"+g.Activity) {
			die(fmt.Errorf("wrong fixture package"))
		}
		g.Starts++
		g.Running = true
		g.Foreground = g.Package
		die(filesafe.JSON(path, g))
		fmt.Println("Status: ok")
		return true
	}
	if strings.Contains(command, "screencap") {
		screen := filepath.Join(root, "screens", strconv.Itoa(index), g.Page+".png")
		if _, e := os.Stat(screen); os.IsNotExist(e) {
			screen = filepath.Join(root, "screens", g.Page+".png")
		}
		b, e := os.ReadFile(screen)
		die(e)
		_, e = os.Stdout.Write(b)
		die(e)
		return true
	}
	if strings.Contains(command, "input tap") {
		fields := strings.Fields(command)
		x, e := strconv.Atoi(fields[len(fields)-2])
		die(e)
		y, e := strconv.Atoi(fields[len(fields)-1])
		die(e)
		g.Taps++
		if !g.Stall {
			switch g.Page {
			case "notice":
				if x >= 465 && x <= 495 && y >= 470 && y <= 490 {
					g.Page = "tree"
				} else {
					die(fmt.Errorf("bad notice tap"))
				}
			case "tree":
				if x >= 735 && x <= 765 && y >= 395 && y <= 415 {
					g.Page = "roles"
				} else {
					die(fmt.Errorf("bad tree tap"))
				}
			case "roles", "selected_1", "selected_2", "selected_3", "selected_4", "selected_5":
				if x >= 24 && x < 56 && y >= 8 && y < 34 {
					g.Page = "tree"
				} else if x >= 125 && x <= 155 {
					ys := []int{95, 169, 240, 311, 382}
					n := 0
					for i, cy := range ys {
						if y >= cy-12 && y <= cy+12 {
							n = i + 1
						}
					}
					if n == 0 {
						die(fmt.Errorf("bad role tap"))
					}
					g.Role = n
					if g.WrongRole {
						g.Role = n%5 + 1
					}
					g.Page = fmt.Sprintf("selected_%d", g.Role)
				} else if x >= 835 && x <= 865 && y >= 478 && y <= 498 && g.Role > 0 {
					g.Page = fmt.Sprintf("game_%d", g.Role)
				} else {
					die(fmt.Errorf("bad enter tap"))
				}
			case "game_1", "game_2", "game_3", "game_4", "game_5":
				if x >= 900 && x < 930 && y >= 310 && y < 334 {
					g.Page = "menu"
				} else {
					die(fmt.Errorf("bad menu tap"))
				}
			case "menu":
				if x >= 800 && x < 830 && y >= 400 && y < 424 {
					g.Page = "settings"
				} else {
					die(fmt.Errorf("bad settings tap"))
				}
			case "settings":
				if x >= 600 && x < 630 && y >= 420 && y < 444 {
					g.Page = "logout_confirm"
				} else {
					die(fmt.Errorf("bad logout tap"))
				}
			case "logout_confirm":
				if x >= 500 && x < 530 && y >= 350 && y < 374 {
					g.Page = "notice"
				} else {
					die(fmt.Errorf("bad logout confirmation"))
				}

			default:
				die(fmt.Errorf("unexpected game action in %s", g.Page))
			}
		}
		die(filesafe.JSON(path, g))
		return true
	}
	return false
}
