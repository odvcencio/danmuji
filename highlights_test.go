package danmuji

import (
	"os"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestHighlightQueryMatchesGenerated(t *testing.T) {
	base := GoGrammar()
	ext := DanmujiGrammar()
	generated := strings.TrimSpace(GenerateHighlightQueries(base, ext))
	fileBytes, err := os.ReadFile("highlights.scm")
	if err != nil {
		t.Fatalf("read highlights.scm: %v", err)
	}
	file := strings.TrimSpace(string(fileBytes))
	if generated != file {
		t.Fatalf("highlights.scm is out of sync with GenerateHighlightQueries\n\nexpected:\n%s\n\nactual:\n%s", generated, file)
	}
}

func TestHighlightQueryCompiles(t *testing.T) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		t.Fatalf("getDanmujiLanguage: %v", err)
	}
	query, err := os.ReadFile("highlights.scm")
	if err != nil {
		t.Fatalf("read highlights.scm: %v", err)
	}
	if _, err = gotreesitter.NewQuery(string(query), lang); err != nil {
		t.Fatalf("NewQuery(highlights.scm): %v", err)
	}
}
