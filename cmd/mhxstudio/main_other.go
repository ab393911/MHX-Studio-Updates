//go:build !windows

package main

import (
	"fmt"
	"os"
)

var version = "3.5.0-preview.0"
var sourceCommit = "development"

func main() {
	fmt.Fprintln(os.Stderr, "MHX Studio GUI requires Windows x64. Use go test ./... for portable tests.")
}
