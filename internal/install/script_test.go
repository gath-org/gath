package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptRejectsMissingTools(t *testing.T) {
	script := findScript(t)
	empty := posixTools(t)

	out, err := runScript(t, script, empty, "check_git")
	if err == nil || !strings.Contains(out, "Git is required") || !strings.Contains(out, "sudo apt install git") {
		t.Fatalf("err %v\n%s", err, out)
	}

	out, err = runScript(t, script, empty, "check_go")
	if err == nil || !strings.Contains(out, "Go 1.24") || !strings.Contains(out, "https://go.dev/dl/") {
		t.Fatalf("err %v\n%s", err, out)
	}

	out, err = runScript(t, script, empty, "detect_os", "GATH_UNAME=FreeBSD")
	if err == nil || !strings.Contains(out, "macOS and Linux") || !strings.Contains(out, "FreeBSD") {
		t.Fatalf("err %v\n%s", err, out)
	}
}

func TestScriptAcceptsCurrentGoAndRejectsOldGo(t *testing.T) {
	script := findScript(t)
	old := fakeGo(t, "#!/bin/sh\nprintf '%s\\n' 'go version go1.22.2 linux/amd64'\n")
	out, err := runScript(t, script, old, "check_go")
	if err == nil || !strings.Contains(out, "Go 1.24 or newer") || !strings.Contains(out, "go1.22.2") {
		t.Fatalf("err %v\n%s", err, out)
	}

	good := fakeGo(t, "#!/bin/sh\nprintf '%s\\n' 'go version go1.24.2 darwin/arm64'\n")
	if _, err := runScript(t, script, good, "check_go"); err != nil {
		t.Fatal(err)
	}
	linux := fakeGo(t, "#!/bin/sh\nprintf '%s\\n' 'go version go1.24.0 linux/amd64'\n")
	if _, err := runScript(t, script, linux, "check_go"); err != nil {
		t.Fatal(err)
	}
	out, err = runScript(t, script, posixTools(t), "detect_os", "GATH_UNAME=Darwin")
	if err != nil || strings.TrimSpace(out) != "Darwin" {
		t.Fatalf("err %v\n%s", err, out)
	}
	out, err = runScript(t, script, posixTools(t), "detect_os", "GATH_UNAME=Linux")
	if err != nil || strings.TrimSpace(out) != "Linux" {
		t.Fatalf("err %v\n%s", err, out)
	}
}

func findScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "scripts", "install.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("scripts/install.sh not found")
		}
		dir = parent
	}
}

func fakeGo(t *testing.T, body string) string {
	t.Helper()
	dir := posixTools(t)
	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func posixTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"awk", "cut", "uname", "cat"} {
		src, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(src, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runScript(t *testing.T, script, path, call string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", `. "$GATH_SCRIPT"; `+call)
	env := []string{
		"PATH=" + path,
		"HOME=" + t.TempDir(),
		"GATH_INSTALL_SOURCE_ONLY=1",
		"GATH_SCRIPT=" + script,
	}
	cmd.Env = append(env, extra...)
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}
