package reposource

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerSupportRemoved(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{".dockerignore", "Dockerfile", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("Docker support file must not exist: %s", name)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func TestNonWebSourcesAreGo(t *testing.T) {
	root := repoRoot(t)
	skipDirs := map[string]bool{
		".git": true, "auths": true, "bin": true, "cloudflared": true,
		"data": true, "dist": true, "legacy": true, "node_modules": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && (skipDirs[entry.Name()] || rel == "web" || rel == "internal/admin/dist") {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".py", ".cmd", ".bat", ".ps1":
			t.Errorf("non-Go runtime source is not allowed: %s", rel)
		case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".vue":
			t.Errorf("JavaScript/TypeScript is only allowed under web/: %s", rel)
		case ".sh":
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.HasPrefix(raw, []byte("\x7fELF")) && !bytes.HasPrefix(raw, []byte("MZ")) {
				t.Errorf("shell source is not allowed: %s", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
