// Package register adds danmuji to the gotreesitter grammars registry so
// the .dmj extension is discoverable by registry consumers (editors, gts).
// Blank-import it to opt in:
//
//	import _ "m31labs.dev/danmuji/register"
//
// It lives in a subpackage so the danmuji core (transpile, format, the
// grammar DSL) stays free of the gotreesitter grammars registry (~200 grammars,
// ~22MB). Only code that wants .dmj registered pays for it.
package register

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"m31labs.dev/danmuji"
)

func init() {
	base := danmuji.GoGrammar()
	ext := danmuji.DanmujiGrammar()
	grammars.RegisterExtension(grammars.ExtensionEntry{
		Name:       "danmuji",
		Extensions: []string{".dmj"},
		Aliases:    []string{"dmj"},
		GenerateLanguage: func() (*gotreesitter.Language, error) {
			return danmuji.GenerateLanguage(ext)
		},
		HighlightQuery: danmuji.GenerateHighlightQueries(base, ext),
	})
}
