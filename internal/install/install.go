// Package install builds the gath binary and puts it on PATH.
package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// PathMarker marks the shell line this installer adds. A later run leaves that line in place.
const PathMarker = "# gath-path"

// DefaultBinDir is where the binary is written when no prefix is chosen.
func DefaultBinDir(home string) string {
	return filepath.Join(home, ".local", "bin")
}

// GoVersionOK reports whether a `go version` line is new enough to build Gath.
// A devel build is accepted. Release builds must be 1.24 or newer.
func GoVersionOK(report string) bool {
	ver := ""
	for _, field := range strings.Fields(report) {
		if strings.HasPrefix(field, "go1.") || strings.HasPrefix(field, "devel") {
			ver = field
			break
		}
	}
	if strings.HasPrefix(ver, "devel") {
		return true
	}
	ver = strings.TrimPrefix(ver, "go")
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 1 || (major == 1 && minor >= 24)
}

// DirOnPath reports whether dir is an entry in a PATH string.
func DirOnPath(pathEnv, dir string) bool {
	dir = filepath.Clean(dir)
	for _, entry := range filepath.SplitList(pathEnv) {
		if entry == "" {
			continue
		}
		if filepath.Clean(entry) == dir {
			return true
		}
	}
	return false
}

// RCFile is the interactive shell file for shell, which is usually $SHELL.
// An empty shell uses zsh on macOS and bash on Linux.
// The boolean is false when this installer does not know that shell.
func RCFile(home, shell string) (string, bool) {
	if shell == "" {
		switch runtime.GOOS {
		case "darwin":
			shell = "zsh"
		case "linux":
			shell = "bash"
		default:
			return "", false
		}
	}
	switch filepath.Base(shell) {
	case "zsh":
		return filepath.Join(home, ".zshrc"), true
	case "bash":
		return filepath.Join(home, ".bashrc"), true
	default:
		return "", false
	}
}

// ExportBlock is the shell text that puts binDir on PATH.
func ExportBlock(home, binDir string) string {
	shown := binDir
	if home != "" {
		rel, err := filepath.Rel(home, binDir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			shown = filepath.Join("$HOME", rel)
		}
	}
	return PathMarker + "\nexport PATH=\"" + shown + ":$PATH\"\n"
}

// MergePathRC appends the PATH block when the marker is not already present.
func MergePathRC(existing, home, binDir string) (string, bool) {
	if strings.Contains(existing, PathMarker) {
		return existing, false
	}
	block := ExportBlock(home, binDir)
	if existing == "" {
		return block, true
	}
	if !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return existing + "\n" + block, true
}

// Result is what Place did.
type Result struct {
	BinPath   string
	RCFile    string
	RCChanged bool
	OnPath    bool
}

// Options controls one install.
type Options struct {
	Home    string
	PathEnv string
	Shell   string
	BinDir  string
	Source  string
	// Build writes the gath binary to out. Nil uses go build.
	Build func(ctx context.Context, source, out string) error
}

// Place builds gath into BinDir and records the directory on PATH when needed.
func Place(ctx context.Context, opt Options) (Result, error) {
	if opt.Home == "" {
		return Result{}, fmt.Errorf("home directory is required")
	}
	if opt.Source == "" {
		return Result{}, fmt.Errorf("source directory is required")
	}
	binDir := opt.BinDir
	if binDir == "" {
		binDir = DefaultBinDir(opt.Home)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return Result{}, err
	}
	binPath := filepath.Join(binDir, "gath")
	tmp := binPath + ".tmp"
	build := opt.Build
	if build == nil {
		build = GoBuild
	}
	if err := build(ctx, opt.Source, tmp); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	if err := os.Rename(tmp, binPath); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	res := Result{BinPath: binPath, OnPath: DirOnPath(opt.PathEnv, binDir)}
	if res.OnPath {
		return res, nil
	}
	rc, ok := RCFile(opt.Home, opt.Shell)
	if !ok {
		return res, nil
	}
	existing, err := os.ReadFile(rc)
	if err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("installed %s, but could not read %s: %w", binPath, rc, err)
	}
	next, changed := MergePathRC(string(existing), opt.Home, binDir)
	if !changed {
		res.RCFile = rc
		return res, nil
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(rc); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(rc, []byte(next), mode); err != nil {
		return res, fmt.Errorf("installed %s, but could not update %s: %w\n\nAdd this to your shell configuration:\n\n%s", binPath, rc, err, ExportBlock(opt.Home, binDir))
	}
	res.RCFile = rc
	res.RCChanged = true
	return res, nil
}

// GoBuild compiles ./cmd/gath from source into out.
func GoBuild(ctx context.Context, source, out string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/gath")
	cmd.Dir = source
	raw, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return fmt.Errorf("build failed: %w", err)
		}
		return fmt.Errorf("build failed: %s", msg)
	}
	return nil
}

// Summary is the text shown after a successful install.
func Summary(res Result, home, binDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Installed %s\n\n", res.BinPath)
	b.WriteString("gath opens the terminal on mainnet.\n\n")
	switch {
	case res.OnPath:
		b.WriteString("Run:\n\n  gath\n")
	case res.RCChanged:
		fmt.Fprintf(&b, "PATH was updated in %s.\nOpen a new terminal, then run:\n\n  gath\n", res.RCFile)
	default:
		fmt.Fprintf(&b, "Add this to your shell configuration:\n\n%s\nThen open a new terminal and run:\n\n  gath\n", ExportBlock(home, binDir))
	}
	return b.String()
}
