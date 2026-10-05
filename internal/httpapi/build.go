package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
)

// Build identifies the code a Bridge runs. Revision and Modified are the Go
// toolchain's VCS stamp: `go build` in a Git checkout sets them, `go run` leaves
// them empty, and Modified follows `git status --porcelain`, so untracked files
// alone make a build dirty. ClientBuild is the content hash the Vite build
// stamps into index.html (web/plugins/buildId.ts); it is read per request so a
// rebuilt web/dist shows on the next poll without a restart.
type Build struct {
	Revision    string `json:"revision"`
	Modified    bool   `json:"modified"`
	ClientBuild string `json:"client_build"`
}

func (s *Server) SetBuild(build Build) { s.build = build }

func BuildFromInfo(info *debug.BuildInfo) Build {
	if info == nil {
		return Build{}
	}
	return buildFromSettings(info.Settings)
}

func buildFromSettings(settings []debug.BuildSetting) Build {
	var build Build
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			build.Revision = setting.Value
		case "vcs.modified":
			build.Modified = setting.Value == "true"
		}
	}
	return build
}

var clientBuildPattern = regexp.MustCompile(`<meta name="herdr-build" content="([0-9a-f]{16})"`)

func clientBuild(staticDir string) string {
	if staticDir == "" {
		return ""
	}
	index, err := os.ReadFile(filepath.Join(staticDir, "index.html"))
	if err != nil {
		return ""
	}
	match := clientBuildPattern.FindSubmatch(index)
	if match == nil {
		return ""
	}
	return string(match[1])
}

// Build is the stamp plus the client build currently in the static dir.
func (s *Server) Build() Build {
	build := s.build
	build.ClientBuild = clientBuild(s.staticDir)
	return build
}
