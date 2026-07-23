package utils

func StrTruncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// StripNonBMP removes characters outside the Basic Multilingual Plane
// (code points above U+FFFF, e.g. emoji). Downstream systems that store text
// sent to the payment gateway may back onto MySQL columns that are not utf8mb4
// and reject 4-byte UTF-8 sequences, failing the whole charge. All BMP text
// (Hebrew, Cyrillic, CJK, accents) is preserved unchanged.
func StripNonBMP(s string) string {
	hasNonBMP := false
	for _, r := range s {
		if r > 0xFFFF {
			hasNonBMP = true
			break
		}
	}
	if !hasNonBMP {
		return s
	}

	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r > 0xFFFF {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
