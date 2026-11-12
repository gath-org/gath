package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gath-org/gath/internal/install"
)

func main() {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	yes := fs.Bool("yes", false, "install without prompting")
	prefix := fs.String("prefix", "", "directory for the gath binary")
	src := fs.String("src", "", "module source directory")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if err := run(context.Background(), *yes, *prefix, *src); err != nil {
		fmt.Fprintf(os.Stderr, "gath install: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, yes bool, prefix, src string) error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("home directory is required")
	}
	if prefix == "" {
		prefix = install.DefaultBinDir(home)
	}
	if src == "" {
		src, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	opt := install.Options{
		Home:    home,
		PathEnv: os.Getenv("PATH"),
		Shell:   os.Getenv("SHELL"),
		BinDir:  prefix,
		Source:  src,
	}
	if yes {
		res, err := install.Place(ctx, opt)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stdout, install.Summary(res, home, prefix))
		return nil
	}
	goVer := goVersion(ctx)
	model := install.NewModel(home, prefix, src, goVer, func(ctx context.Context) (install.Result, error) {
		return install.Place(ctx, opt)
	})
	program := tea.NewProgram(model)
	final, err := program.Run()
	if err != nil {
		return err
	}
	done, ok := final.(install.Model)
	if !ok {
		return fmt.Errorf("install screen ended unexpectedly")
	}
	return done.Err()
}

func goVersion(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "go", "version")
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
