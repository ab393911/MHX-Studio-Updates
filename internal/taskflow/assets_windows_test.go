//go:build windows

package taskflow

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

// The fixture contains only the currency fields from handoff image
// 74. Friend names, avatars and all account-specific content are removed.
func TestNativeWindowsAssetAmounts(t *testing.T) {
	b, e := os.ReadFile("testdata/assets_query.b64")
	if e != nil {
		t.Fatal(e)
	}
	b, e = base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	reader := vision.NewNativeReader()
	defer reader.Close()
	v, e := vision.ReadAmounts(ctx, reader, b, assetAreas)
	if e != nil {
		raw, readErr := vision.ReadFields(ctx, reader, b, assetAreas)
		t.Fatalf("native amount parsing: %v; raw=%q; fields=%v", e, raw, readErr)
	}
	silver, merit, available, e := assetAmounts(v)
	if e != nil || silver != 320000 || merit != 300 || available != 30000 {
		t.Fatalf("native OCR: %+v; %d %d %d; %v", v, silver, merit, available, e)
	}
}
