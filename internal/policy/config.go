package policy

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Retention      time.Duration
	StorageDir     string
	ResourceLimits ResourceLimits
	Rules          Rules
}

type ResourceLimits struct {
	MaxRAMBytes        int64
	MaxDiskBytes       int64
	MaxSnapshotsPerApp int
}

type Rules struct {
	ExcludeExeNames  []string
	ExcludePathSubstr []string
	ExcludeWindowClasses []string
}

func (r Rules) Allow(exePath string, windowClass string) bool {
	exeLower := strings.ToLower(filepath.Base(exePath))
	pathLower := strings.ToLower(exePath)
	classLower := strings.ToLower(windowClass)

	for _, n := range r.ExcludeExeNames {
		if exeLower == strings.ToLower(n) {
			return false
		}
	}
	for _, s := range r.ExcludePathSubstr {
		if s == "" {
			continue
		}
		if strings.Contains(pathLower, strings.ToLower(s)) {
			return false
		}
	}
	for _, c := range r.ExcludeWindowClasses {
		if c == "" {
			continue
		}
		if classLower == strings.ToLower(c) {
			return false
		}
	}
	return true
}

func DefaultConfig() *Config {
	base := defaultStorageDir()
	return &Config{
		Retention: 24 * time.Hour,
		StorageDir: base,
		ResourceLimits: ResourceLimits{
			MaxRAMBytes:        256 * 1024 * 1024,   // 256MB in-memory target
			MaxDiskBytes:       2 * 1024 * 1024 * 1024, // 2GB spillover
			MaxSnapshotsPerApp: 500,
		},
		Rules: Rules{
			ExcludeExeNames:       []string{"keepass.exe"},
			ExcludePathSubstr:     []string{`\\AppData\\Local\\Temp\\`},
			ExcludeWindowClasses:  []string{},
		},
	}
}

func defaultStorageDir() string {
	// %LOCALAPPDATA%\Rewinder2\Snapshots
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return filepath.Join(v, "Rewinder2", "Snapshots")
	}
	if v := os.Getenv("APPDATA"); v != "" {
		return filepath.Join(v, "Rewinder2", "Snapshots")
	}
	return filepath.Join(".", "snapshots")
}

