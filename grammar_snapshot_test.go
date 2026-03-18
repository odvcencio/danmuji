package danmuji

import (
	"bytes"
	"os"
	"testing"
)

func TestGrammarJSONMatchesGenerated(t *testing.T) {
	generated, err := ExportGrammarJSON(DanmujiGrammar())
	if err != nil {
		t.Fatalf("ExportGrammarJSON: %v", err)
	}

	fileBytes, err := os.ReadFile("testdata/grammar.json")
	if err != nil {
		t.Fatalf("read testdata/grammar.json: %v", err)
	}

	if !bytes.Equal(bytes.TrimSpace(generated), bytes.TrimSpace(fileBytes)) {
		t.Fatalf("testdata/grammar.json is out of sync with ExportGrammarJSON(DanmujiGrammar())")
	}
}
