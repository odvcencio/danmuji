package danmuji

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func init() {
	base := GoGrammar()
	ext := DanmujiGrammar()
	grammars.RegisterExtension(grammars.ExtensionEntry{
		Name:       "danmuji",
		Extensions: []string{".dmj"},
		Aliases:    []string{"dmj"},
		GenerateLanguage: func() (*gotreesitter.Language, error) {
			return GenerateLanguage(ext)
		},
		HighlightQuery: GenerateHighlightQueries(base, ext),
	})
}
