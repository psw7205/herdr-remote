package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/stream"
)

func TestBuildFromSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings []debug.BuildSetting
		want     Build
	}{
		{"go run leaves no stamp", []debug.BuildSetting{{Key: "-buildmode", Value: "exe"}}, Build{}},
		{"clean build", []debug.BuildSetting{{Key: "vcs.revision", Value: "1a3f949e7addebe8aba571217e1baa56af912e3d"}, {Key: "vcs.modified", Value: "false"}}, Build{Revision: "1a3f949e7addebe8aba571217e1baa56af912e3d"}},
		{"dirty build", []debug.BuildSetting{{Key: "vcs.revision", Value: "1a3f949e7addebe8aba571217e1baa56af912e3d"}, {Key: "vcs.modified", Value: "true"}}, Build{Revision: "1a3f949e7addebe8aba571217e1baa56af912e3d", Modified: true}},
	}
	for _, tc := range cases {
		if got := buildFromSettings(tc.settings); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if got := BuildFromInfo(nil); got != (Build{}) {
		t.Errorf("nil info: got %+v", got)
	}
}

func TestClientBuildReadsIndexMeta(t *testing.T) {
	dir := t.TempDir()
	if got := clientBuild(dir); got != "" {
		t.Fatalf("missing index.html: got %q", got)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`<html><head><meta name="herdr-build" content="__BUILD_ID__" /></head></html>`)
	if got := clientBuild(dir); got != "" {
		t.Fatalf("dev placeholder: got %q", got)
	}
	write(`<html><head><meta name="herdr-build" content="0123456789abcdef" /><title>x</title></head></html>`)
	if got := clientBuild(dir); got != "0123456789abcdef" {
		t.Fatalf("stamped: got %q", got)
	}
	write(`<html><head><meta name="herdr-build" content="0123456789ABCDEF" /></head></html>`)
	if got := clientBuild(dir); got != "" {
		t.Fatalf("non-hex: got %q", got)
	}
}

func TestSessionsReportsBridgeBuild(t *testing.T) {
	store, err := command.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	static := t.TempDir()
	if err = os.WriteFile(filepath.Join(static, "index.html"), []byte(`<meta name="herdr-build" content="fedcba9876543210" />`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := New(&fakeSessions{hub: stream.New("claude:native")}, store, []string{"http://localhost:5173"}, static)
	api.SetBuild(Build{Revision: "65f5c6bd7eceb1cd601405dec32f04078918f161", Modified: true})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	res, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Bridge Build `json:"bridge"`
	}
	if err = json.NewDecoder(res.Body).Decode(&body); err != nil || res.StatusCode != 200 {
		t.Fatal(res.StatusCode, err)
	}
	want := Build{Revision: "65f5c6bd7eceb1cd601405dec32f04078918f161", Modified: true, ClientBuild: "fedcba9876543210"}
	if body.Bridge != want {
		t.Fatalf("bridge = %+v, want %+v", body.Bridge, want)
	}
	// A rebuilt dist shows on the next poll without a restart.
	if err = os.WriteFile(filepath.Join(static, "index.html"), []byte(`<meta name="herdr-build" content="0000000000000001" />`), 0o644); err != nil {
		t.Fatal(err)
	}
	res2, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if err = json.NewDecoder(res2.Body).Decode(&body); err != nil || body.Bridge.ClientBuild != "0000000000000001" {
		t.Fatalf("after rebuild: %+v %v", body.Bridge, err)
	}
}
