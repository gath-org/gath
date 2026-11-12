package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoVersionOK(t *testing.T) {
	ok := []string{
		"go version go1.24.0 linux/amd64",
		"go version go1.24.2 darwin/arm64",
		"go version go1.25.0 linux/amd64",
		"go version devel go1.25-abcdef linux/amd64",
	}
	for _, line := range ok {
		if !GoVersionOK(line) {
			t.Fatalf("want ok: %s", line)
		}
	}
	bad := []string{
		"",
		"go version go1.23.9 linux/amd64",
		"go version go1.22.2 linux/amd64",
		"not go",
	}
	for _, line := range bad {
		if GoVersionOK(line) {
			t.Fatalf("want reject: %s", line)
		}
	}
}

func TestDirOnPath(t *testing.T) {
	dir := filepath.Join("/home/gath", ".local", "bin")
	if !DirOnPath("/usr/bin:"+dir+":/bin", dir) {
		t.Fatal("expected the directory on PATH")
	}
	if DirOnPath("/usr/bin:/bin", dir) {
		t.Fatal("directory is not on PATH")
	}
	if DefaultBinDir("/home/gath") != dir {
		t.Fatal(DefaultBinDir("/home/gath"))
	}
}

func TestMergePathRCOnce(t *testing.T) {
	home := "/home/gath"
	bin := DefaultBinDir(home)
	next, changed := MergePathRC("", home, bin)
	if !changed || !strings.Contains(next, PathMarker) || !strings.Contains(next, `$HOME/.local/bin:$PATH`) {
		t.Fatalf("changed %v\n%s", changed, next)
	}
	again, changed := MergePathRC(next, home, bin)
	if changed || again != next {
		t.Fatal("marker was added twice")
	}
	rc, ok := RCFile(home, "/bin/zsh")
	if !ok || rc != filepath.Join(home, ".zshrc") {
		t.Fatalf("zsh %s %v", rc, ok)
	}
	rc, ok = RCFile(home, "/bin/bash")
	if !ok || rc != filepath.Join(home, ".bashrc") {
		t.Fatalf("bash %s %v", rc, ok)
	}
	if _, ok = RCFile(home, "/bin/fish"); ok {
		t.Fatal("fish should be left to the operator")
	}
	rc, ok = RCFile(home, "")
	if runtime.GOOS == "linux" && (!ok || rc != filepath.Join(home, ".bashrc")) {
		t.Fatalf("empty shell %s %v", rc, ok)
	}
}

func TestPlaceInstallsAndRecordsPath(t *testing.T) {
	home := t.TempDir()
	binDir := DefaultBinDir(home)
	opt := Options{
		Home:    home,
		PathEnv: "/usr/bin:/bin",
		Shell:   "/bin/zsh",
		BinDir:  binDir,
		Source:  t.TempDir(),
		Build: func(_ context.Context, _, out string) error {
			return os.WriteFile(out, []byte("#!/bin/sh\n"), 0o644)
		},
	}
	res, err := Place(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.BinPath != filepath.Join(binDir, "gath") || !res.RCChanged || res.OnPath {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(Summary(res, home, binDir), "PATH was updated in "+filepath.Join(home, ".zshrc")) {
		t.Fatal(Summary(res, home, binDir))
	}
	st, err := os.Stat(res.BinPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary is not executable: %s", st.Mode())
	}
	rc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rc), PathMarker) != 1 {
		t.Fatalf("rc:\n%s", rc)
	}
	res, err = Place(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.RCChanged {
		t.Fatal("second install rewrote PATH")
	}
	rc, err = os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rc), PathMarker) != 1 {
		t.Fatalf("rc:\n%s", rc)
	}
	text := Summary(res, home, binDir)
	if !strings.Contains(text, "gath opens the terminal on mainnet") || !strings.Contains(text, res.BinPath) {
		t.Fatalf("%s", text)
	}

	opt.PathEnv = binDir
	res, err = Place(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OnPath {
		t.Fatal("directory is on PATH")
	}
	if !strings.Contains(Summary(res, home, binDir), "\n  gath\n") {
		t.Fatal(Summary(res, home, binDir))
	}
}
