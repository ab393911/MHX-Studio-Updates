//go:build !windows

package main

func main() { panic("windowsverify requires a native Windows runner") }
