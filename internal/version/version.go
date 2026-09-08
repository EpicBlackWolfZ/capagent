// Package version provides runtime and build-time metadata for capagent.
package version

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// Build-time metadata injected via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "local"
	Vendor  = "EpicBlackWolfZ"
	OS      = runtime.GOOS
	Arch    = runtime.GOARCH
)

// ANSI color escape codes for terminal styling.
const (
	ColorReset  = "\033[0m"
	ColorCyan   = "\033[36m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBold   = "\033[1m"
)

// AsciiLogo provides the terminal banner ASCII art for capagent.
const AsciiLogo = `
  ___ __ _ _ __   __ _  __ _  ___ _ __  | |_ 
 / __/ _` + "`" + ` | '_ \ / _` + "`" + ` |/ _` + "`" + ` |/ _ \ '_ \ | __|
| (_| (_| | |_) | (_| | (_| |  __/ | | || |_ 
 \___\__,_| .__/ \__,_|\__, |\___|_| |_(_)__|
          |_|          |___/                 
`

// BuildInfo captures all build and runtime metadata.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuiltBy   string `json:"built_by"`
	Vendor    string `json:"vendor"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
}

// Get returns the current BuildInfo struct.
func Get() BuildInfo {
	return BuildInfo{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		BuiltBy:   BuiltBy,
		Vendor:    Vendor,
		OS:        OS,
		Arch:      Arch,
		GoVersion: runtime.Version(),
	}
}

// Info returns formatted version and build details string.
func Info() string {
	return fmt.Sprintf("capagent version %s (commit: %s, built: %s by %s, %s, %s/%s)",
		Version, Commit, Date, BuiltBy, runtime.Version(), OS, Arch)
}

// PrintStartupBanner writes an ASCII art logo and build metadata to the specified writer.
func PrintStartupBanner(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	_, _ = fmt.Fprintf(w, "%s%s%s\n", ColorCyan, AsciiLogo, ColorReset)
	_, _ = fmt.Fprintf(w, "  %-12s: %s%s%s\n", "Version", ColorGreen, Version, ColorReset)
	_, _ = fmt.Fprintf(w, "  %-12s: %s\n", "Commit", Commit)
	_, _ = fmt.Fprintf(w, "  %-12s: %s\n", "Build Date", Date)
	_, _ = fmt.Fprintf(w, "  %-12s: %s\n", "Built By", BuiltBy)
	_, _ = fmt.Fprintf(w, "  %-12s: %s\n", "Vendor", Vendor)
	_, _ = fmt.Fprintf(w, "  %-12s: %s/%s\n", "Platform", OS, Arch)
	_, _ = fmt.Fprintf(w, "  %-12s: %s\n\n", "Go Version", runtime.Version())
}
