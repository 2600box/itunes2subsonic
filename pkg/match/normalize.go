package match

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	bracketedQualifier = regexp.MustCompile(`(?i)\([^)]*\)|\[[^\]]*\]|\{[^}]*\}`)
	bonusTrackPattern  = regexp.MustCompile(`(?i)\bbonus\s+track\b`)
)

var noiseTokens = map[string]struct{}{
	"feat":       {},
	"ft":         {},
	"featuring":  {},
	"remaster":   {},
	"remastered": {},
	"bonus":      {},
	"demo":       {},
	"live":       {},
	"remix":      {},
	"mix":        {},
	"version":    {},
	"edit":       {},
}

func NormalizeText(input string) string {
	if input == "" {
		return ""
	}
	text := strings.ToLower(input)
	text = strings.ReplaceAll(text, "&", " and ")
	text = bonusTrackPattern.ReplaceAllString(text, " ")
	text = bracketedQualifier.ReplaceAllString(text, " ")
	text = norm.NFKD.String(text)
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	tokens := strings.Fields(b.String())
	if len(tokens) == 0 {
		return ""
	}
	filtered := tokens[:0]
	for _, token := range tokens {
		if _, ok := noiseTokens[token]; ok {
			continue
		}
		filtered = append(filtered, token)
	}
	return strings.Join(filtered, " ")
}

func Tokens(normalized string) []string {
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}
