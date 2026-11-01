package main

import (
	"context"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
)

// A continuation belongs to the complete selected device and role, not just its
// list position/index. UI callbacks and completion both recheck this snapshot.
type gameTarget struct {
	Console, ADB              string
	Index, PID, Role          int
	TopWindow, BindWindow     uint64
	Serial, AndroidID, BootID string
}

func targetForGame(c ldplayer.Client, d ldplayer.Instance, role int) gameTarget {
	return gameTarget{c.Console, c.ADB, d.Index, d.PID, role, d.TopWindow, d.BindWindow, d.Serial, d.AndroidID, d.BootID}
}

type gameContinuation struct {
	Context  context.Context
	Target   gameTarget
	Action   int
	Discover bool
	consumed bool
}

func (g *gameContinuation) Allowed(current gameTarget) bool {
	return g != nil && !g.consumed && g.Context.Err() == nil && g.Target == current
}
func (g *gameContinuation) Take(current gameTarget) bool {
	if !g.Allowed(current) {
		return false
	}
	g.consumed = true
	return true
}

func (g *gameContinuation) Apply(current gameTarget, effect func()) bool {
	if !g.Take(current) {
		return false
	}
	effect()
	return true
}
