package danmuji

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMakeTagRefusesToTagOnRed is the acceptance test for the item 1
// tagging guard: `make tag` must refuse to create a git tag when the test
// suite (or vet) is red, and must succeed and create the tag once the
// build is green. It exercises the repository's real Makefile (via `make
// -f`) against a small synthetic Go module so the check runs in
// milliseconds instead of re-running danmuji's own multi-minute suite.
func TestMakeTagRefusesToTagOnRed(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	makefilePath := filepath.Join(repoRoot, "Makefile")
	if _, err := os.Stat(makefilePath); err != nil {
		t.Fatalf("Makefile missing: %v", err)
	}

	dir := t.TempDir()

	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = goEnv()
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// A git repo is required: `make tag` runs `git tag`.
	if out, err := run("git", "init", "-q"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := run("git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v\n%s", err, out)
	}
	if out, err := run("git", "config", "user.name", "test"); err != nil {
		t.Fatalf("git config name: %v\n%s", err, out)
	}

	goMod := "module tagguard\n\ngo 1.24.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	redTest := `package tagguard

import "testing"

func TestAlwaysFails(t *testing.T) {
	t.Fatal("deliberately red")
}
`
	testPath := filepath.Join(dir, "guard_test.go")
	if err := os.WriteFile(testPath, []byte(redTest), 0644); err != nil {
		t.Fatalf("write red test: %v", err)
	}
	if out, err := run("git", "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := run("git", "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// Red: `make tag` must fail and must NOT create the tag.
	out, err := run("make", "-C", dir, "-f", makefilePath, "tag", "VERSION=v9.9.9")
	if err == nil {
		t.Fatalf("expected `make tag` to fail on a red suite; output:\n%s", out)
	}
	t.Logf("red make tag output:\n%s", out)

	tagsOut, tagErr := run("git", "tag", "-l")
	if tagErr != nil {
		t.Fatalf("git tag -l: %v", tagErr)
	}
	if strings.Contains(tagsOut, "v9.9.9") {
		t.Fatalf("tag v9.9.9 must not exist after a red `make tag`; tags:\n%s", tagsOut)
	}

	// Fix the test (green) and try again: `make tag` must succeed and the
	// tag must exist afterward.
	greenTest := `package tagguard

import "testing"

func TestAlwaysFails(t *testing.T) {
	// now passes
}
`
	if err := os.WriteFile(testPath, []byte(greenTest), 0644); err != nil {
		t.Fatalf("write green test: %v", err)
	}

	out, err = run("make", "-C", dir, "-f", makefilePath, "tag", "VERSION=v9.9.9")
	if err != nil {
		t.Fatalf("expected `make tag` to succeed on a green suite: %v\noutput:\n%s", err, out)
	}
	t.Logf("green make tag output:\n%s", out)

	tagsOut, tagErr = run("git", "tag", "-l")
	if tagErr != nil {
		t.Fatalf("git tag -l: %v", tagErr)
	}
	if !strings.Contains(tagsOut, "v9.9.9") {
		t.Fatalf("expected tag v9.9.9 to exist after a green `make tag`; tags:\n%s", tagsOut)
	}
}

// TestMakeTagRejectsMalformedVersion checks the VERSION format guard runs
// even before the (expensive) release-gate prerequisite would matter in
// practice — it must not create a tag for a non-semver-looking VERSION.
func TestMakeTagRejectsMalformedVersion(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	makefilePath := filepath.Join(repoRoot, "Makefile")

	dir := t.TempDir()
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = goEnv()
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("git", "init", "-q"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := run("git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v\n%s", err, out)
	}
	if out, err := run("git", "config", "user.name", "test"); err != nil {
		t.Fatalf("git config name: %v\n%s", err, out)
	}
	goMod := "module tagguard2\n\ngo 1.24.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	greenTest := "package tagguard2\n\nimport \"testing\"\n\nfunc TestPasses(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "guard_test.go"), []byte(greenTest), 0644); err != nil {
		t.Fatalf("write test: %v", err)
	}
	if out, err := run("git", "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := run("git", "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	out, err := run("make", "-C", dir, "-f", makefilePath, "tag", "VERSION=not-a-version")
	if err == nil {
		t.Fatalf("expected `make tag` to reject a malformed VERSION; output:\n%s", out)
	}

	tagsOut, tagErr := run("git", "tag", "-l")
	if tagErr != nil {
		t.Fatalf("git tag -l: %v", tagErr)
	}
	if strings.TrimSpace(tagsOut) != "" {
		t.Fatalf("expected no tags to be created; got:\n%s", tagsOut)
	}
}
