package danmuji

import (
	"strings"
	"testing"
)

func TestFormatDanmujiBasic(t *testing.T) {
	// Messy indentation — should be normalized to tabs.
	source := []byte(`package main_test

import "testing"

unit "arithmetic" {
  given "two numbers" {
    a := 2
      b := 3
        when "added" {
          result := a + b
            then "equals sum" {
              expect result == 5
            }
        }
  }
}
`)

	formatted, err := FormatDanmuji(source)
	if err != nil {
		t.Fatalf("FormatDanmuji: %v", err)
	}
	t.Logf("Formatted:\n%s", formatted)

	// Should contain tab indentation.
	if !strings.Contains(formatted, "\t") {
		t.Error("expected tab indentation in formatted output")
	}
	// Should still contain all the key constructs.
	for _, kw := range []string{"package", "import", "unit", "given", "when", "then", "expect"} {
		if !strings.Contains(formatted, kw) {
			t.Errorf("expected %q in formatted output", kw)
		}
	}
	// Should end with newline.
	if !strings.HasSuffix(formatted, "\n") {
		t.Error("expected trailing newline")
	}
}

func TestFormatDanmujiPreservesBlankLines(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "test1" {
	then "ok" {
		expect true
	}
}

unit "test2" {
	then "ok" {
		expect true
	}
}
`)

	formatted, err := FormatDanmuji(source)
	if err != nil {
		t.Fatalf("FormatDanmuji: %v", err)
	}
	t.Logf("Formatted:\n%s", formatted)

	// Should preserve blank line between test blocks.
	if !strings.Contains(formatted, "}\n\nunit") {
		t.Error("expected blank line between test blocks")
	}
}

func TestFormatDanmujiIdempotent(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "test" {
	given "a value" {
		x := 42
		then "it is 42" {
			expect x == 42
		}
	}
}
`)

	first, err := FormatDanmuji(source)
	if err != nil {
		t.Fatalf("first format: %v", err)
	}
	second, err := FormatDanmuji([]byte(first))
	if err != nil {
		t.Fatalf("second format: %v", err)
	}
	if first != second {
		t.Errorf("format is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
