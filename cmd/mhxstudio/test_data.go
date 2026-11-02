package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Empty for normal builds. Standalone acceptance builds set this using -X.
// This changes only their data location, never the user configuration schema.
var isolatedTestID string

func dataDirectory(base, requested string) (string, error) {
	if isolatedTestID == "" {
		if requested != "" {
			return requested, nil
		}
		return filepath.Join(base, "MHXStudioNext"), nil
	}
	// The identifier comes from the build, and must remain one path component.
	if strings.ContainsAny(isolatedTestID, `/\\:`) || isolatedTestID == "." || isolatedTestID == ".." {
		return "", fmt.Errorf("独立测试版目录标识无效")
	}
	root := filepath.Join(base, "MHXStudioTests", isolatedTestID)
	if requested != "" && !strings.EqualFold(filepath.Clean(requested), filepath.Clean(root)) {
		return "", fmt.Errorf("独立测试版仅使用专用数据目录 %s；拒绝访问指定目录，未修改任何配置或数据库", root)
	}
	return root, nil
}
