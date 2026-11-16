package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"voicerewrite/internal/contractgen"
)

func main() {
	check := flag.Bool("check", false, "fail when a generated host file differs")
	flag.Parse()
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	files, err := contractgen.Generate(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	failed := false
	for path, content := range files {
		if *check {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != content {
				fmt.Fprintf(os.Stderr, "generated file is out of date: %s\n", path)
				failed = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
