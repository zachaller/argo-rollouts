package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// sourceFile is one Go test file, read either from the working tree or from a git ref.
type sourceFile struct {
	// Path is relative to the repository root, with forward slashes.
	Path string
	Src  []byte
}

// loadTestFiles returns every *_test.go file under dirs. With an empty ref the
// working tree is read; otherwise the files are read from that git ref.
func loadTestFiles(ref string, dirs []string) ([]sourceFile, error) {
	if ref == "" {
		return loadTestFilesFromTree(dirs)
	}
	return loadTestFilesFromGit(ref, dirs)
}

func loadTestFilesFromTree(dirs []string) ([]sourceFile, error) {
	var files []sourceFile
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "vendor", "node_modules", ".git":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files = append(files, sourceFile{Path: filepath.ToSlash(path), Src: src})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadTestFilesFromGit(ref string, dirs []string) ([]sourceFile, error) {
	args := append([]string{"ls-tree", "-r", "--name-only", ref, "--"}, dirs...)
	out, err := runGit(args...)
	if err != nil {
		return nil, err
	}
	var files []sourceFile
	for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasSuffix(path, "_test.go") {
			continue
		}
		if strings.Contains(path, "/vendor/") || strings.HasPrefix(path, "vendor/") {
			continue
		}
		src, err := runGit("show", ref+":"+path)
		if err != nil {
			return nil, err
		}
		files = append(files, sourceFile{Path: path, Src: src})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func runGit(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
