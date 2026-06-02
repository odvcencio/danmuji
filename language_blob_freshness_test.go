package danmuji

import (
	"bytes"
	"testing"
)

// TestEmbeddedBlobIsFresh guards against silent staleness of the embedded
// parser blob.
//
// The grammar-JSON hash in language.hash only changes when the GRAMMAR
// changes, not when the gotreesitter compiler does. So a gotreesitter
// version bump can silently leave an old blob embedded: it still decodes
// (the blob format is forward-compatible), but misses newer table-quality
// improvements, and loadEmbeddedLanguageBlob's hash check never fires.
//
// This test recompiles the blob from the live compiler and fails if it
// differs from the committed language.bin, instructing you to run
// `go generate ./...`. GenerateLanguageAndBlob is byte-deterministic, so
// the comparison is stable. The runtime path (loadEmbeddedLanguageBlob) is
// intentionally left untouched, so downstream consumers — whose module
// resolution may pull a different gotreesitter version — never see a
// spurious staleness error; the freshness gate lives only in danmuji's CI,
// where the pinned compiler version is authoritative.
func TestEmbeddedBlobIsFresh(t *testing.T) {
	g := DanmujiGrammar()
	_, blob, err := GenerateLanguageAndBlob(g)
	if err != nil {
		t.Fatalf("regenerate blob from live compiler: %v", err)
	}
	if !bytes.Equal(blob, embeddedLanguageBlob) {
		t.Fatalf("embedded language.bin is stale (recompiled %d bytes vs embedded %d); "+
			"run 'go generate ./...' to refresh the blob after a gotreesitter bump",
			len(blob), len(embeddedLanguageBlob))
	}
}
