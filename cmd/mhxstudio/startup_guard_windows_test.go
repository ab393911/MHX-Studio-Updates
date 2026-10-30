//go:build windows

package main

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"testing"
)

func TestOnOpenCannotStartBeforeHealthAndUpdateCommit(t *testing.T) {
	a := &app{cfg: defaults(), lastInventory: "detected"}
	a.cfg.Startup = &login.StartupPlan{Enabled: true}
	if a.tryOnOpen() || a.startupAttempted {
		t.Fatal("started before health")
	}
	a.healthDone = true
	a.updateResultPath = "pending"
	a.updateResultDone = false
	if a.tryOnOpen() || a.startupAttempted {
		t.Fatal("started before update commit")
	}
	a.updateResultDone = true
	a.cfg.Startup = nil
	if a.tryOnOpen() || a.startupAttempted {
		t.Fatal("guessed a startup plan")
	}
}
