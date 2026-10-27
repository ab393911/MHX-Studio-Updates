// Test fixture only: Windows CI builds this as an EXE that exits immediately.
// It is never included in user distributions.
package main

import (
	"flag"
	"os"
	"path/filepath"
)

func main() {
	var root, receipt, token, version, result string
	flag.StringVar(&root, "data-dir", "", "")
	flag.StringVar(&receipt, "health-receipt", "", "")
	flag.StringVar(&token, "health-token", "", "")
	flag.StringVar(&version, "health-version", "", "")
	flag.StringVar(&result, "update-result", "", "")
	flag.Parse()
	if os.Getenv("MHX_TEST_MUTATE_CONFIG") == "1" && root != "" {
		_ = os.WriteFile(filepath.Join(root, "config.json"), []byte("failed native migration"), 0600)
		_ = os.MkdirAll(filepath.Join(root, "profiles"), 0700)
		_ = os.WriteFile(filepath.Join(root, "profiles", "partial-new.json"), []byte("partial"), 0600)
	}
}
