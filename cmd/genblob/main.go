// Command genblob generates the embedded language blob and hash for the
// danmuji grammar.  Run via "go generate" from the repository root.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/odvcencio/danmuji"
)

func main() {
	g := danmuji.DanmujiGrammar()

	jsonData, err := danmuji.ExportGrammarJSON(g)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export grammar JSON: %v\n", err)
		os.Exit(1)
	}

	sum := sha256.Sum256(jsonData)
	hash := hex.EncodeToString(sum[:])

	_, blob, err := danmuji.GenerateLanguageAndBlob(g)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate language blob: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("language.bin", blob, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write language.bin: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("language.hash", []byte(hash+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write language.hash: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote language.bin (%d bytes) and language.hash (%s)\n", len(blob), hash[:24]+"...")
}
