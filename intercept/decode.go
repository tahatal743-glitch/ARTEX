package intercept

// Encoded-input decoding for the rule engine.
//
// Rules used to be matched against the raw tool_input JSON blob only, so a
// target written as %67ov.cn, \x67ov.cn, %u0067ov.cn or as a base64 token
// slipped past a rule that names gov.cn in plain text. This file adds the
// "decode, then match" pass: every rule is evaluated against the original text
// plus its decoded variants, so a rule keeps its old matches and gains the
// encoded ones.
//
// Adapted from CyberStrikeAI internal/toolguard/toolguard.go (Apache-2.0):
// candidateTexts, decodePercentEscapes and hexValue. The reference walks decoded
// JSON values and unwraps nested percent escapes; on top of that this port adds
// \xNN, %uNNNN / \uNNNN and base64 passes, because ARTEX rules are matched
// against a JSON blob rather than a typed argument map.
//
// The pass is bounded on purpose: candidateTexts is called on every tool call
// that enters the rule engine, so the number of candidates, the number of decode
// rounds and the size of a base64 token are all capped.

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// decodeRounds is how many times the escape decoder is applied to one
	// candidate, so nested encodings (%252E → %2E → .) are unwrapped. The
	// reference implementation uses three rounds as well.
	decodeRounds = 3
	// maxCandidates bounds the work per tool call: every rule is matched
	// against every candidate.
	maxCandidates = 256
	// maxJSONDepth bounds the walk over decoded tool_input JSON.
	maxJSONDepth = 32
	// maxBase64Tokens bounds how many base64-looking tokens are decoded per call.
	maxBase64Tokens = 32
	// maxBase64TokenLen / minBase64TokenLen bound a single token. The lower
	// bound keeps short ordinary words out of the decoder.
	maxBase64TokenLen = 8192
	minBase64TokenLen = 16
	// minBase64Decoded is the shortest decoded payload worth adding as a
	// candidate.
	minBase64Decoded = 8
)

// candidateTexts returns the strings a rule is matched against for one subject:
// the original text first (so nothing that matched before stops matching), then
// its decoded variants.
func candidateTexts(raw string) []string {
	c := &candidateSet{seen: make(map[string]struct{}, 8)}
	c.add(raw)
	c.addJSON(raw)
	c.addBase64()
	return c.list
}

// candidateSet collects candidates, keeping the first-seen order and dropping
// duplicates.
type candidateSet struct {
	list    []string
	seen    map[string]struct{}
	b64Used int
}

func (c *candidateSet) full() bool { return len(c.list) >= maxCandidates }

// push records value and reports whether it was newly added.
func (c *candidateSet) push(value string) bool {
	if value == "" || c.full() {
		return false
	}
	if _, ok := c.seen[value]; ok {
		return false
	}
	c.seen[value] = struct{}{}
	c.list = append(c.list, value)
	return true
}

// add records value and each successive decode round applied to it, stopping
// early when a round changes nothing.
func (c *candidateSet) add(value string) {
	for round := 0; round <= decodeRounds; round++ {
		c.push(value)
		if c.full() {
			return
		}
		decoded := decodeEscapes(value)
		if decoded == value {
			return
		}
		value = decoded
	}
}

// addJSON inspects decoded JSON so an escape that only exists after JSON
// unescaping (for example "%" standing for a percent sign) is still visible
// to the rules. It adds the compact re-marshaled form, then every string value
// and key, mirroring the reference walk.
func (c *candidateSet) addJSON(raw string) {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return
	}
	if compact, err := json.Marshal(value); err == nil {
		c.add(string(compact))
	}
	c.walk(value, maxJSONDepth)
}

func (c *candidateSet) walk(value any, depth int) {
	if depth <= 0 || c.full() {
		return
	}
	switch v := value.(type) {
	case string:
		c.add(v)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c.add(k)
			c.walk(v[k], depth-1)
		}
	case []any:
		for _, item := range v {
			c.walk(item, depth-1)
		}
	}
}

// addBase64 decodes base64-looking tokens found in the candidates collected so
// far. Only one level is unwrapped: the snapshot is taken before the loop so a
// decoded payload is not itself scanned again.
func (c *candidateSet) addBase64() {
	snapshot := c.list
	for _, candidate := range snapshot {
		for _, token := range base64TokenPattern.FindAllString(candidate, maxBase64Tokens) {
			if c.b64Used >= maxBase64Tokens || c.full() {
				return
			}
			c.b64Used++
			if decoded, ok := decodeBase64Token(token); ok {
				c.add(decoded)
			}
		}
	}
}

// base64TokenPattern matches the base64 alphabet (standard and URL-safe) in a
// run long enough to be a payload rather than a word.
var base64TokenPattern = regexp.MustCompile(`[A-Za-z0-9+/_-]{16,}={0,2}`)

// decodeBase64Token decodes one token, rejecting anything whose payload is not
// mostly printable text — that filter is what keeps long hex ids, hashes and
// random identifiers from producing binary noise as candidates.
func decodeBase64Token(token string) (string, bool) {
	token = strings.TrimRight(token, "=")
	if len(token) < minBase64TokenLen || len(token) > maxBase64TokenLen {
		return "", false
	}
	var (
		decoded []byte
		err     error
	)
	if strings.ContainsAny(token, "-_") {
		decoded, err = base64.RawURLEncoding.DecodeString(token)
	} else {
		decoded, err = base64.RawStdEncoding.DecodeString(token)
	}
	if err != nil || len(decoded) < minBase64Decoded || !isPrintableText(decoded) {
		return "", false
	}
	return string(decoded), true
}

// isPrintableText reports whether b is valid UTF-8 that is at least 90%
// printable (or plain whitespace).
func isPrintableText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	total, printable := 0, 0
	for _, r := range string(b) {
		total++
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsPrint(r) {
			printable++
		}
	}
	return total > 0 && printable*10 >= total*9
}

// decodeEscapes performs one decoding round over value: percent triplets (the
// reference's decoder, tolerant of a stray percent elsewhere in the string),
// JavaScript-style %uNNNN, and the \xNN / \uNNNN escapes that appear in shell
// and JSON payloads. Input that decodes to itself is returned unchanged, which
// is how the callers detect that they can stop.
func decodeEscapes(value string) string {
	if !strings.ContainsAny(value, "%\\") {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		if value[i] == '%' {
			if i+5 < len(value) && (value[i+1] == 'u' || value[i+1] == 'U') {
				if r, ok := hex4(value[i+2:]); ok {
					out.WriteRune(r)
					i += 6
					continue
				}
			}
			if i+2 < len(value) {
				if hi, okHi := hexValue(value[i+1]); okHi {
					if lo, okLo := hexValue(value[i+2]); okLo {
						out.WriteByte(hi<<4 | lo)
						i += 3
						continue
					}
				}
			}
			out.WriteByte(value[i])
			i++
			continue
		}
		if value[i] == '\\' {
			if i+3 < len(value) && (value[i+1] == 'x' || value[i+1] == 'X') {
				if hi, okHi := hexValue(value[i+2]); okHi {
					if lo, okLo := hexValue(value[i+3]); okLo {
						out.WriteByte(hi<<4 | lo)
						i += 4
						continue
					}
				}
			}
			if i+5 < len(value) && value[i+1] == 'u' {
				if r, ok := hex4(value[i+2:]); ok {
					// A surrogate pair (😀) decodes as one rune.
					if utf16.IsSurrogate(r) && i+11 < len(value) && value[i+6] == '\\' && value[i+7] == 'u' {
						if low, okLow := hex4(value[i+8:]); okLow {
							if combined := utf16.DecodeRune(r, low); combined != utf8.RuneError {
								out.WriteRune(combined)
								i += 12
								continue
							}
						}
					}
					out.WriteRune(r)
					i += 6
					continue
				}
			}
			out.WriteByte(value[i])
			i++
			continue
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

// hex4 decodes four hexadecimal digits at the start of s.
func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		v, ok := hexValue(s[i])
		if !ok {
			return 0, false
		}
		r = r<<4 | rune(v)
	}
	return r, true
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}
