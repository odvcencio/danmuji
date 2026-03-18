package danmuji

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func danmujiLanguageCachePath(g *Grammar) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	jsonData, err := ExportGrammarJSON(g)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(jsonData)
	name := "language-" + hex.EncodeToString(sum[:12]) + ".bin"
	return filepath.Join(cacheDir, "danmuji", name), nil
}

func loadCachedDanmujiLanguageBlob(g *Grammar) ([]byte, error) {
	path, err := danmujiLanguageCachePath(g)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func storeCachedDanmujiLanguageBlob(g *Grammar, blob []byte) error {
	path, err := danmujiLanguageCachePath(g)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, blob, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
