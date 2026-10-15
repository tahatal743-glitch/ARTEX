package server

// Argv construction for kind=command tools that declare parameters.
//
// Ported from CyberStrikeAI internal/security/executor.go buildCommandArgs
// (Apache-2.0) so the imported tool recipes keep their argument semantics:
//
//  1. fixed args from the spec (unless ArgsReplaceParam was supplied, e.g. nmap's
//     scan_type replaces the default -sT -sV -sC)
//  2. the position-0 positional param (subcommand: `gobuster dir ...`)
//  3. flag / combined / template params in declared order; bool params append
//     their flag only when true and nothing when false
//  4. positional params in ascending position order (positions 1..N)
//  5. additional_args, split like a shell, appended last
//
// Unlike CSI (which execs the binary directly) the resulting argv is shell-quoted
// element by element and handed to the Bash tool, so every recipe inherits the
// Bash path's intercept rules, timeout, output spill and proxy env.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// buildToolArgv renders the final argument vector for one tool call, command
// name first, so the caller only has to shell-quote and join it.
func buildToolArgv(spec commandExec, params map[string]any) ([]string, error) {
	argv := make([]string, 0, 9+len(spec.Params)*2)
	argv = append(argv, spec.Command)

	replaced := false
	if spec.ArgsReplaceParam != "" {
		if v := paramString(params[spec.ArgsReplaceParam]); v != "" {
			argv = append(argv, splitArgs(v)...)
			replaced = true
		}
	}
	if !replaced {
		argv = append(argv, spec.Args...)
	}

	special := func(name string) bool {
		return name == "additional_args" || name == "action" || name == spec.ArgsReplaceParam
	}
	// value resolves a param: supplied value, else declared default; ok=false means
	// "not supplied and no default".
	value := func(p cmdParam) (any, bool) {
		if v, exists := params[p.Name]; exists && v != nil {
			if s, isStr := v.(string); !isStr || strings.TrimSpace(s) != "" {
				return v, true
			}
		}
		if p.Default != nil {
			return p.Default, true
		}
		return nil, false
	}

	var positional, flags []cmdParam
	for _, p := range spec.Params {
		if p.Position != nil {
			positional = append(positional, p)
		} else {
			flags = append(flags, p)
		}
	}

	// Position 0: a subcommand goes before every flag (CSI executor.go:397).
	for _, p := range positional {
		if p.Position == nil || *p.Position != 0 || special(p.Name) {
			continue
		}
		v, ok := value(p)
		if !ok {
			if p.Required {
				return nil, fmt.Errorf("缺少必需参数 %s", p.Name)
			}
			break
		}
		argv = append(argv, formatParamValue(p, v))
		break
	}

	for _, p := range flags {
		if special(p.Name) {
			continue
		}
		v, ok := value(p)
		if !ok {
			if p.Required {
				return nil, fmt.Errorf("缺少必需参数 %s", p.Name)
			}
			continue
		}
		if p.Type == "bool" {
			if !boolish(v) {
				continue // false adds nothing at all
			}
			if p.Flag != "" {
				argv = append(argv, p.Flag)
			}
			continue
		}
		fv := formatParamValue(p, v)
		if strings.TrimSpace(fv) == "" {
			continue
		}
		switch p.Format {
		case "combined": // --flag=value
			if p.Flag != "" {
				argv = append(argv, p.Flag+"="+fv)
			} else {
				argv = append(argv, fv)
			}
		case "template": // expand {flag}/{value}/{name}, then split on spaces
			if p.Template != "" {
				argv = append(argv, splitArgs(expandParamTemplate(p.Template, p, fv))...)
			} else if p.Flag != "" {
				argv = append(argv, p.Flag, fv)
			} else {
				argv = append(argv, fv)
			}
		default: // "flag" or empty: flag then value
			if p.Flag != "" {
				argv = append(argv, p.Flag)
			}
			argv = append(argv, fv)
		}
	}

	maxPos := -1
	for _, p := range positional {
		if p.Position != nil && *p.Position > maxPos {
			maxPos = *p.Position
		}
	}
	for i := 1; i <= maxPos; i++ {
		for _, p := range positional {
			if p.Position == nil || *p.Position != i || special(p.Name) {
				continue
			}
			v, ok := value(p)
			if !ok {
				if p.Required {
					return nil, fmt.Errorf("缺少必需参数 %s", p.Name)
				}
				break // this slot stays empty; later slots keep their order
			}
			argv = append(argv, formatParamValue(p, v))
			break
		}
	}

	if s := paramString(params["additional_args"]); s != "" {
		argv = append(argv, splitArgs(s)...)
	}
	return argv, nil
}

// expandParamTemplate mirrors CSI's template substitution (executor.go:483).
func expandParamTemplate(tmpl string, p cmdParam, value string) string {
	out := strings.ReplaceAll(tmpl, "{flag}", p.Flag)
	out = strings.ReplaceAll(out, "{value}", value)
	out = strings.ReplaceAll(out, "{name}", p.Name)
	return out
}

// formatParamValue mirrors CSI's formatParamValue (executor.go:715): bools as
// true/false, arrays comma-joined, objects as JSON, and port lists without spaces
// (nmap rejects "80,443, 22").
func formatParamValue(p cmdParam, v any) string {
	switch p.Type {
	case "bool":
		if b, ok := v.(bool); ok {
			return fmt.Sprintf("%v", b)
		}
		return "false"
	case "array":
		if arr, ok := v.([]any); ok {
			parts := make([]string, 0, len(arr))
			for _, item := range arr {
				parts = append(parts, fmt.Sprintf("%v", item))
			}
			return strings.Join(parts, ",")
		}
	case "object":
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	out := fmt.Sprintf("%v", v)
	// JSON numbers arrive as float64, and %v switches to exponent notation at 1e6
	// and above ("1e+06") — masscan rates are routinely quoted in the millions, so
	// render integral floats in plain decimal. Only values that would otherwise
	// print with an exponent change.
	if f, ok := v.(float64); ok {
		out = strconv.FormatFloat(f, 'f', -1, 64)
	}
	if p.Name == "ports" {
		out = strings.ReplaceAll(out, " ", "")
	}
	return out
}

func boolish(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return true
		}
	}
	return false
}

func paramString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// splitArgs splits a free-text argument string the way a shell would: whitespace
// separates, single/double quotes group, backslash escapes the next rune. Ported
// from CSI's parseAdditionalArgs (executor.go:641) with the same observable rules.
func splitArgs(s string) []string {
	var (
		out     []string
		cur     strings.Builder
		inWord  bool
		quote   rune // 0 = none, '\'' or '"'
		escaped bool
	)
	flush := func() {
		if inWord {
			out = append(out, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			inWord = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			inWord = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inWord = true
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			inWord = true
		default:
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				flush()
				continue
			}
			cur.WriteRune(r)
			inWord = true
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	flush()
	return out
}

// shellJoin renders argv as one shell command line, quoting every element so a
// value can never smuggle shell syntax through the recipe.
func shellJoin(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}
