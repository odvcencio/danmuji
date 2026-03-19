package processrunmeta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if err := os.MkdirAll("bin", 0755); err != nil {
		panic(err)
	}

	outputPath := filepath.Join("bin", "readytool")
	buildCmd := exec.Command("go", "build", "-o", outputPath, "./cmd/readytool")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		panic(err)
	}

	code := m.Run()
	_ = os.Remove(outputPath)
	os.Exit(code)
}
