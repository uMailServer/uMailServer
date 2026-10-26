package api

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"github.com/umailserver/umailserver/internal/db"
)

// Webmail filters are applied at delivery by compiling them to a Sieve
// script (RFC 5228) that the delivery path runs like a ManageSieve script.
// Before this they were stored and listed but never applied.
//
// Semantics of the generated script:
//   - every enabled filter whose conditions match applies its actions, in
//     priority order (there is no implicit stop);
//   - markRead and flag set \Seen and \Flagged on the message wherever it is
//     filed: all flag actions run before any folder action;
//   - delete moves the message to Trash, it is never destroyed at delivery;
//   - forward redirects a copy and keeps the local one;
//   - string comparisons are case-insensitive (i;ascii-casemap) and
//     "matches" is a regular expression.

// filterSieveRequires lists the Sieve extensions the generated script uses.
const filterSieveRequires = `require ["fileinto", "copy", "imap4flags", "body", "regex"];`

// headerNameRE is the RFC 5322 field-name syntax (printable ASCII but colon).
var headerNameRE = regexp.MustCompile(`^[!-9;-~]+$`)

// LoadUserFilters returns the filters stored for user, ordered by priority.
func LoadUserFilters(d *db.DB, user string) ([]*EmailFilter, error) {
	var filters []*EmailFilter
	err := d.ForEachPrefix(db.BucketFilters, user+":", func(key string, value []byte) error {
		var filter EmailFilter
		if err := json.Unmarshal(value, &filter); err != nil {
			return nil // Skip invalid entries
		}
		filters = append(filters, &filter)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// F5694: order by the stored priority (set by reorder), then creation time;
	// the key order (random UUIDs) used to be returned and the priority ignored.
	sort.SliceStable(filters, func(i, j int) bool {
		if filters[i].Priority != filters[j].Priority {
			return filters[i].Priority < filters[j].Priority
		}
		if !filters[i].CreatedAt.Equal(filters[j].CreatedAt) {
			return filters[i].CreatedAt.Before(filters[j].CreatedAt)
		}
		return filters[i].ID < filters[j].ID
	})
	for i := range filters {
		filters[i].Priority = i
	}
	return filters, nil
}

// FiltersToSieve compiles the enabled filters to a Sieve script. It returns
// "" when no enabled filter has an action to apply. Conditions or actions
// that cannot be expressed (unknown types, invalid regex or address) are
// skipped; a filter left without conditions or actions is skipped.
func FiltersToSieve(filters []*EmailFilter) string {
	var flagRules, folderRules []string
	for _, f := range filters {
		if f == nil || !f.Enabled {
			continue
		}
		test := filterSieveTest(f)
		if test == "" {
			continue
		}
		var flagCmds, folderCmds []string
		for _, a := range f.Actions {
			cmd, isFlag := filterSieveAction(a)
			switch {
			case cmd == "":
			case isFlag:
				flagCmds = append(flagCmds, cmd)
			default:
				folderCmds = append(folderCmds, cmd)
			}
		}
		if len(flagCmds) > 0 {
			flagRules = append(flagRules, sieveIf(test, flagCmds))
		}
		if len(folderCmds) > 0 {
			folderRules = append(folderRules, sieveIf(test, folderCmds))
		}
	}
	if len(flagRules) == 0 && len(folderRules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(filterSieveRequires + "\n")
	for _, r := range append(flagRules, folderRules...) {
		b.WriteString(r)
	}
	return b.String()
}

func sieveIf(test string, cmds []string) string {
	return "if " + test + " {\n  " + strings.Join(cmds, "\n  ") + "\n}\n"
}

// filterSieveTest returns the Sieve test for the conditions of f, or "" when
// none of them can be expressed.
func filterSieveTest(f *EmailFilter) string {
	var tests []string
	for _, c := range f.Conditions {
		if t := filterSieveCondition(c); t != "" {
			tests = append(tests, t)
		}
	}
	// A dropped condition must not widen what the filter matches: with
	// matchAll every condition has to be expressible.
	if len(tests) == 0 || (f.MatchAll && len(tests) != len(f.Conditions)) {
		return ""
	}
	if len(tests) == 1 {
		return tests[0]
	}
	op := "anyof"
	if f.MatchAll {
		op = "allof"
	}
	return op + "(" + strings.Join(tests, ", ") + ")"
}

func filterSieveCondition(c FilterCondition) string {
	if c.Value == "" {
		return ""
	}
	var match, key string
	switch c.Operator {
	case "contains":
		match, key = ":contains", c.Value
	case "equals":
		match, key = ":is", c.Value
	case "startsWith":
		match, key = ":matches", sieveGlobEscape(c.Value)+"*"
	case "endsWith":
		match, key = ":matches", "*"+sieveGlobEscape(c.Value)
	case "matches":
		if _, err := regexp.Compile(c.Value); err != nil {
			return ""
		}
		match, key = ":regex", c.Value
	default:
		return ""
	}
	switch c.Field {
	case "from":
		return "header " + match + " \"from\" " + sieveQuote(key)
	case "to":
		return "header " + match + " [\"to\", \"cc\"] " + sieveQuote(key)
	case "subject":
		return "header " + match + " \"subject\" " + sieveQuote(key)
	case "header":
		if !headerNameRE.MatchString(c.HeaderName) {
			return ""
		}
		return "header " + match + " " + sieveQuote(c.HeaderName) + " " + sieveQuote(key)
	case "body":
		return "body :text " + match + " " + sieveQuote(key)
	}
	return ""
}

// filterSieveAction returns the Sieve command for a, and whether it only
// sets flags. It returns "" for an action that cannot be expressed.
func filterSieveAction(a FilterAction) (cmd string, isFlag bool) {
	switch a.Type {
	case "markRead":
		return `addflag "\\Seen";`, true
	case "flag":
		return `addflag "\\Flagged";`, true
	case "move":
		if strings.EqualFold(a.Target, "INBOX") {
			return "keep;", false
		}
		if !validFilterFolder(a.Target) {
			return "", false
		}
		return "fileinto " + sieveQuote(a.Target) + ";", false
	case "copy":
		if strings.EqualFold(a.Target, "INBOX") || !validFilterFolder(a.Target) {
			return "", false
		}
		return "fileinto :copy " + sieveQuote(a.Target) + ";", false
	case "delete":
		return `fileinto "Trash";`, false
	case "markSpam":
		return `fileinto "Junk";`, false
	case "forward":
		addr, err := mail.ParseAddress(a.ForwardTo)
		if err != nil || addr.Name != "" || !strings.Contains(addr.Address, "@") {
			return "", false
		}
		return "redirect :copy " + sieveQuote(addr.Address) + ";", false
	}
	return "", false
}

// validFilterFolder accepts a non-empty mailbox name without control
// characters.
func validFilterFolder(name string) bool {
	if strings.TrimSpace(name) == "" || len(name) > 255 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// sieveQuote returns s as a Sieve quoted string (RFC 5228 §2.4.2).
func sieveQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// sieveGlobEscape escapes the :matches wildcards in s so it matches
// literally (RFC 5228 §2.7.1).
func sieveGlobEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `*`, `\*`)
	return strings.ReplaceAll(s, `?`, `\?`)
}

// validateFilterRule checks the field, operator and action shapes that
// FiltersToSieve can apply. It returns an error message, or "".
func validateFilterRule(conditions []FilterCondition, actions []FilterAction) string {
	for i, c := range conditions {
		switch c.Field {
		case "from", "to", "subject", "body":
		case "header":
			if !headerNameRE.MatchString(c.HeaderName) {
				return fmt.Sprintf("condition %d has an invalid headerName", i+1)
			}
		default:
			return fmt.Sprintf("condition %d has an unknown field %q", i+1, c.Field)
		}
		switch c.Operator {
		case "contains", "equals", "startsWith", "endsWith":
		case "matches":
			if _, err := regexp.Compile(c.Value); err != nil {
				return fmt.Sprintf("condition %d has an invalid regular expression", i+1)
			}
		default:
			return fmt.Sprintf("condition %d has an unknown operator %q", i+1, c.Operator)
		}
	}
	for i, a := range actions {
		switch a.Type {
		case "markRead", "flag", "delete", "markSpam":
		case "move", "copy":
			if !validFilterFolder(a.Target) {
				return fmt.Sprintf("action %d requires a target folder", i+1)
			}
		case "forward":
			if cmd, _ := filterSieveAction(a); cmd == "" {
				return fmt.Sprintf("action %d requires a valid forwardTo address", i+1)
			}
		default:
			return fmt.Sprintf("action %d has an unknown type %q", i+1, a.Type)
		}
	}
	return ""
}
