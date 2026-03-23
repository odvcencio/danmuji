package danmuji

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var sharedTestModuleDir string

func TestMain(m *testing.M) {
	if _, err := getDanmujiLanguage(); err != nil {
		fmt.Fprintf(os.Stderr, "prime danmuji language: %v\n", err)
		os.Exit(1)
	}

	dir, err := prepareSharedTestModule()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare shared test module: %v\n", err)
		os.Exit(1)
	}
	sharedTestModuleDir = dir

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func prepareSharedTestModule() (string, error) {
	tmpDir, err := os.MkdirTemp("", "danmuji-testmod-")
	if err != nil {
		return "", err
	}

	goMod := `module testmod

go 1.24.0

require github.com/stretchr/testify v1.9.0
require github.com/gorilla/websocket v1.5.3
require google.golang.org/grpc v1.76.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return "", err
	}

	placeholder := `package testmod_test

import _ "github.com/stretchr/testify/assert"
import _ "github.com/gorilla/websocket"
import _ "google.golang.org/grpc"
`
	placeholderPath := filepath.Join(tmpDir, "placeholder_test.go")
	if err := os.WriteFile(placeholderPath, []byte(placeholder), 0644); err != nil {
		return "", err
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}

	if err := os.Remove(placeholderPath); err != nil {
		return "", err
	}

	return tmpDir, nil
}

func setupModuleInDir(t *testing.T, dir string) {
	t.Helper()

	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(sharedTestModuleDir, name))
		if err != nil {
			t.Fatalf("read shared %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func newTestModule(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	setupModuleInDir(t, tmpDir)
	return tmpDir
}

func writeModuleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
