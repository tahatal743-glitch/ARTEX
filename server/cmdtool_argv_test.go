package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func intp(i int) *int { return &i }

// nmapSpec mirrors the converted nmap recipe (tools/nmap.yaml).
func nmapSpec() commandExec {
	return commandExec{
		Command: "nmap",
		Args:    []string{"-sT", "-sV", "-sC"},
		Params: []cmdParam{
			{Name: "target", Type: "string", Position: intp(1), Format: "positional", Required: true},
			{Name: "ports", Type: "string", Flag: "-p", Format: "flag"},
			{Name: "timing", Type: "string", Format: "template", Template: "-T{value}"},
			{Name: "os_detection", Type: "bool", Flag: "-O", Format: "flag", Default: false},
			{Name: "aggressive", Type: "bool", Flag: "-A", Format: "flag", Default: false},
			{Name: "scan_type", Type: "string", Format: "template", Template: "{value}"},
			{Name: "additional_args", Type: "string", Format: "positional"},
		},
		ArgsReplaceParam: "scan_type",
	}
}

func TestBuildToolArgvNmapDefaults(t *testing.T) {
	argv, err := buildToolArgv(nmapSpec(), map[string]any{"target": "127.0.0.1", "ports": "80,443, 8080"})
	if err != nil {
		t.Fatal(err)
	}
	want := "nmap -sT -sV -sC -p 80,443,8080 127.0.0.1"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestBuildToolArgvNmapScanTypeReplacesDefaults(t *testing.T) {
	argv, err := buildToolArgv(nmapSpec(), map[string]any{"target": "10.0.0.5", "scan_type": "-sU -Pn"})
	if err != nil {
		t.Fatal(err)
	}
	want := "nmap -sU -Pn 10.0.0.5"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestBuildToolArgvBoolAndTemplate(t *testing.T) {
	spec := nmapSpec()
	argv, err := buildToolArgv(spec, map[string]any{
		"target": "example.com", "timing": "4", "os_detection": true, "aggressive": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "nmap -sT -sV -sC -T4 -O example.com"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestBuildToolArgvAdditionalArgsAreSplitAndLast(t *testing.T) {
	argv, err := buildToolArgv(nmapSpec(), map[string]any{
		"target": "10.0.0.1", "additional_args": `--max-retries 3 -Pn "a b"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nmap", "-sT", "-sV", "-sC", "10.0.0.1", "--max-retries", "3", "-Pn", "a b"}
	if strings.Join(argv, "|") != strings.Join(want, "|") {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}

func TestBuildToolArgvRequiredMissing(t *testing.T) {
	if _, err := buildToolArgv(nmapSpec(), map[string]any{}); err == nil {
		t.Fatal("missing required target must be an error")
	}
}

func TestBuildToolArgvSubcommandAndCombined(t *testing.T) {
	spec := commandExec{
		Command: "gobuster",
		Params: []cmdParam{
			{Name: "mode", Type: "string", Position: intp(0), Format: "positional", Default: "dir", Options: []string{"dir", "dns"}},
			{Name: "url", Type: "string", Flag: "-u", Format: "flag", Required: true},
			{Name: "threads", Type: "string", Flag: "-t", Format: "combined"},
		},
	}
	// mode is not supplied: its default ("dir") must still land in slot 0.
	argv, err := buildToolArgv(spec, map[string]any{"url": "http://127.0.0.1", "threads": "20"})
	if err != nil {
		t.Fatal(err)
	}
	want := "gobuster dir -u http://127.0.0.1 -t=20"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

// "action" is tool-internal in CSI's recipes (executor.go:381) and must never be
// forwarded to the command, even when it declares a position.
func TestBuildToolArgvActionParamIsNeverPassed(t *testing.T) {
	spec := commandExec{
		Command: "msf",
		Params: []cmdParam{
			{Name: "action", Type: "string", Position: intp(0), Format: "positional"},
			{Name: "url", Type: "string", Flag: "-u", Format: "flag", Required: true},
		},
	}
	argv, err := buildToolArgv(spec, map[string]any{"action": "run", "url": "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	want := "msf -u http://127.0.0.1"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestBuildToolArgvPositionalOrderAndDefaults(t *testing.T) {
	spec := commandExec{
		Command: "sqlmap",
		Params: []cmdParam{
			{Name: "url", Type: "string", Flag: "-u", Format: "flag", Required: true},
			{Name: "second", Type: "string", Position: intp(2), Format: "positional"},
			{Name: "first", Type: "string", Position: intp(1), Format: "positional"},
		},
	}
	argv, err := buildToolArgv(spec, map[string]any{"url": "http://t/?id=1", "first": "A", "second": "B"})
	if err != nil {
		t.Fatal(err)
	}
	want := "sqlmap -u http://t/?id=1 A B"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

// The rendered command line must keep one argv element per token even when a value
// carries spaces or shell metacharacters.
func TestShellJoinQuotesEveryElement(t *testing.T) {
	line := shellJoin([]string{"nmap", "-p", "80 443", "$(reboot)", "a'b"})
	want := `'nmap' '-p' '80 443' '$(reboot)' 'a'\''b'`
	if line != want {
		t.Fatalf("shellJoin = %q, want %q", line, want)
	}
}

// Real-data check: the embedded recipe file parses, every entry has a command, and
// the nmap recipe builds the expected line through the same code path the tool
// uses at runtime.
func TestCSIToolSeedsParseAndNmapBuilds(t *testing.T) {
	var doc struct {
		Tools []csiToolSeed `json:"tools"`
	}
	if err := json.Unmarshal(csiToolSeedsJSON, &doc); err != nil {
		t.Fatalf("csitoolseeds.json 解析失败: %v", err)
	}
	if len(doc.Tools) < 80 {
		t.Fatalf("expected >=80 seeded recipes, got %d", len(doc.Tools))
	}
	var nmap *csiToolSeed
	for i := range doc.Tools {
		if doc.Tools[i].Key == "nmap" {
			nmap = &doc.Tools[i]
		}
		if len(doc.Tools[i].Exec) == 0 {
			t.Fatalf("recipe %s has empty exec", doc.Tools[i].Key)
		}
	}
	if nmap == nil {
		t.Fatal("nmap recipe missing")
	}
	var spec commandExec
	if err := json.Unmarshal(nmap.Exec, &spec); err != nil {
		t.Fatalf("nmap exec 解析失败: %v", err)
	}
	argv, err := buildToolArgv(spec, map[string]any{"target": "192.168.56.10"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "nmap -sT -sV -sC 192.168.56.10" {
		t.Fatalf("argv = %q", got)
	}
	if !strings.Contains(nmap.Description, "网络") {
		t.Fatalf("nmap description lost in conversion: %q", nmap.Description[:40])
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`-v -Pn`, []string{"-v", "-Pn"}},
		{`--data "a b" 'c d'`, []string{"--data", "a b", "c d"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`  spaced   out  `, []string{"spaced", "out"}},
		{``, nil},
	}
	for _, tc := range cases {
		got := splitArgs(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("splitArgs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
