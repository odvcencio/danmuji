package danmuji

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
)

//go:embed language.bin
var embeddedLanguageBlob []byte

//go:embed language.hash
var embeddedLanguageHash string

// loadEmbeddedLanguageBlob returns the embedded blob if the grammar hash
// matches.  Returns a non-nil error if the grammar has changed since the
// blob was generated, indicating that "go generate" must be re-run.
func loadEmbeddedLanguageBlob(g *Grammar) ([]byte, error) {
	jsonData, err := ExportGrammarJSON(g)
	if err != nil {
		return nil, fmt.Errorf("export grammar JSON: %w", err)
	}

	sum := sha256.Sum256(jsonData)
	currentHash := hex.EncodeToString(sum[:])
	expectedHash := strings.TrimSpace(embeddedLanguageHash)

	if currentHash != expectedHash {
		return nil, fmt.Errorf(
			"embedded language blob is stale (want %s, have %s); run 'go generate ./...' to rebuild",
			currentHash, expectedHash,
		)
	}

	return embeddedLanguageBlob, nil
}
