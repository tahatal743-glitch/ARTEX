package intercept

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func regexRule(target, pattern string) compiledRule {
	return compiledRule{
		InterceptRule: db.InterceptRule{MatchTarget: target, MatchType: "regex", Pattern: pattern, Action: "deny"},
		re:            regexp.MustCompile(pattern),
	}
}

func containsCandidate(candidates []string, want string) bool {
	for _, c := range candidates {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

// TestCandidateTextsDecodesEscapes is the P0.2 acceptance table: every encoded
// spelling of gov.cn must appear as a candidate, so a rule naming gov.cn in
// plain text catches it.
func TestCandidateTextsDecodesEscapes(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("curl http://gov.cn/thread"))
	urlB64 := base64.RawURLEncoding.EncodeToString([]byte("GET /gov.cn/x HTTP/1.1"))
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"percent", `{"url":"http://%67ov.cn/a"}`, "gov.cn"},
		{"percent-two-rounds", `{"url":"http://%2567ov.cn/a"}`, "gov.cn"},
		{"percent-three-rounds", `{"url":"%252567ov.cn"}`, "gov.cn"},
		{"percent-stray-sign", `{"note":"100% sure, %67ov.cn"}`, "gov.cn"},
		{"percent-dotted", `curl http://%2E%67%6F%76/`, ".gov"},
		{"hex-escape", `{"cmd":"curl \x67ov.cn"}`, "gov.cn"},
		{"js-percent-u", `{"url":"%u0067ov.cn"}`, "gov.cn"},
		{"json-u-nonjson-input", `\u0067ov.cn`, "gov.cn"},
		{"json-escaped-percent", `{"url":"\u002567ov.cn"}`, "%67ov.cn"},
		{"base64-in-json", `{"cmd":"` + b64 + `"}`, "gov.cn"},
		{"base64-urlsafe-bare", urlB64, "gov.cn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := candidateTexts(tc.raw)
			if !containsCandidate(got, tc.want) {
				t.Fatalf("candidates %q do not contain %q", got, tc.want)
			}
		})
	}
}

// The original text must stay first so that nothing which matched before the
// decode pass existed can stop matching.
func TestCandidateTextsKeepsRawFirst(t *testing.T) {
	raw := `{"command":"curl http://%67ov.cn/"}`
	got := candidateTexts(raw)
	if len(got) == 0 || got[0] != raw {
		t.Fatalf("raw text is not the first candidate: %q", got)
	}
}

// Ordinary input must not sprout decoded noise. The JSON walk still contributes
// the keys and values (that is the reference behaviour, and it is what makes a
// rule match a value that JSON escaping would otherwise hide), so the expected
// set is exact: a Windows path stays a Windows path, a 40-char hex id is not
// mistaken for base64, and a plain command stays as written.
func TestCandidateTextsLeavesPlainInputAlone(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{
			`{"file_path":"C:\\Users\\admin\\notes.txt"}`,
			[]string{`{"file_path":"C:\\Users\\admin\\notes.txt"}`, "file_path", `C:\Users\admin\notes.txt`},
		},
		{
			`{"id":"9f2c1ab34d5e6f708192a3b4c5d6e7f8091a2b3c"}`,
			[]string{`{"id":"9f2c1ab34d5e6f708192a3b4c5d6e7f8091a2b3c"}`, "id", "9f2c1ab34d5e6f708192a3b4c5d6e7f8091a2b3c"},
		},
		{
			`{"command":"ls -la /opt/artex"}`,
			[]string{`{"command":"ls -la /opt/artex"}`, "command", "ls -la /opt/artex"},
		},
	}
	for _, tc := range cases {
		got := candidateTexts(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("candidates for %q = %q, want %q", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("candidate %d for %q = %q, want %q (all: %q)", i, tc.raw, got[i], tc.want[i], got)
			}
		}
	}
}

// A surrogate pair is one rune after decoding, not two replacement characters.
func TestDecodeEscapesSurrogatePair(t *testing.T) {
	got := decodeEscapes(`\uD83D\uDE00 ok`)
	if !strings.Contains(got, "\U0001F600") {
		t.Fatalf("surrogate pair not decoded: %q", got)
	}
}

// The candidate count is capped: a call carrying hundreds of base64 payloads
// must not turn into hundreds of rule evaluations.
func TestCandidateTextsIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"blob":"`)
	for i := 0; i < 400; i++ {
		b.WriteString(base64.StdEncoding.EncodeToString([]byte("payload number gov.cn " + strings.Repeat("x", i%7))))
		b.WriteString(" ")
	}
	b.WriteString(`"}`)
	got := candidateTexts(b.String())
	if len(got) > maxCandidates {
		t.Fatalf("candidate count %d exceeds cap %d", len(got), maxCandidates)
	}
}

// End-to-end through Match: an encoded target is intercepted, and switching the
// pass off restores the exact pre-port behaviour.
func TestMatchDecodesEncodedInput(t *testing.T) {
	input := []byte(`{"command":"curl -s http://%67ov.cn/thread0806.php"}`)
	rules := []compiledRule{regexRule("tool_input", `gov\.cn`)}

	on := &Interceptor{cached: rules, decodeGuard: true}
	dec, ok := on.Match("Bash", input)
	if !ok {
		t.Fatal("encoded target slipped past the rule with the decode pass on")
	}
	if dec.Action != "deny" || dec.RuleName != "" {
		t.Fatalf("unexpected decision: %+v", dec)
	}

	off := &Interceptor{cached: rules, decodeGuard: false}
	if _, matched := off.Match("Bash", input); matched {
		t.Fatal("decode pass off must keep the legacy behaviour (no match here)")
	}
	if _, matched := off.Match("Bash", []byte(`{"command":"curl -s http://gov.cn/"}`)); !matched {
		t.Fatal("plain target must still match with the decode pass off")
	}
}

// ruleMatches itself: a contains rule and a regex rule both see every subject.
func TestRuleMatchesAcrossSubjects(t *testing.T) {
	contains := compiledRule{InterceptRule: db.InterceptRule{MatchTarget: "tool_input", MatchType: "contains", Pattern: "gov.cn"}}
	if !ruleMatches(contains, []string{"Bash"}, candidateTexts(`{"u":"%67ov.cn"}`)) {
		t.Fatal("contains rule missed the decoded subject")
	}
	if ruleMatches(contains, []string{"Bash"}, []string{`{"u":"%67ov.cn"}`}) {
		t.Fatal("contains rule must not match the raw text alone")
	}
	nameRule := regexRule("tool_name", `^Bash$`)
	if !ruleMatches(nameRule, []string{"Bash"}, []string{`{}`}) {
		t.Fatal("tool_name rule stopped matching")
	}
	unknown := compiledRule{InterceptRule: db.InterceptRule{MatchTarget: "other", MatchType: "contains", Pattern: "gov.cn"}}
	if ruleMatches(unknown, []string{"Bash"}, candidateTexts(`{"u":"gov.cn"}`)) {
		t.Fatal("unknown match target must not match")
	}
}
