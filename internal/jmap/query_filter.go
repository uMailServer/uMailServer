package jmap

import (
	"encoding/json"

	"github.com/umailserver/umailserver/internal/storage"
)

// maxFilterDepth bounds FilterOperator nesting so a hostile request cannot
// drive unbounded recursion.
const maxFilterDepth = 16

// supportedEmailFilterKeys are the Email/query FilterCondition properties the
// server actually evaluates. Anything else must be answered with
// unsupportedFilter (RFC 8620 §5.5) instead of being silently ignored, which
// would return every message for a filter the client expects to narrow.
var supportedEmailFilterKeys = map[string]bool{
	"inMailbox": true, "inMailboxOtherThan": true, "before": true,
	"after": true, "minSize": true, "maxSize": true, "hasKeyword": true,
	"notKeyword": true, "text": true, "from": true, "to": true,
	"subject": true,
}

// supportedEmailSortProps are the Email/query comparator properties the
// server implements.
var supportedEmailSortProps = map[string]bool{
	"receivedAt": true, "sentAt": true, "from": true, "to": true,
	"subject": true, "size": true,
}

// queryError is a method-level error type plus description.
type queryError struct{ typ, desc string }

func (e *queryError) response(callID string) Response {
	return Response{
		Name: "error",
		Args: map[string]interface{}{"type": e.typ, "description": e.desc},
		ID:   callID,
	}
}

// validateEmailFilter checks a Filter (FilterOperator or FilterCondition).
func validateEmailFilter(filter interface{}, depth int) *queryError {
	if filter == nil {
		return nil
	}
	m, ok := filter.(map[string]interface{})
	if !ok {
		return &queryError{"invalidArguments", "filter must be an object"}
	}
	if depth > maxFilterDepth {
		return &queryError{"invalidArguments", "filter nested too deeply"}
	}
	if op, has := m["operator"]; has {
		o, _ := op.(string)
		if o != "AND" && o != "OR" && o != "NOT" {
			return &queryError{"invalidArguments", "operator must be AND, OR or NOT"}
		}
		conds, ok := m["conditions"].([]interface{})
		if !ok {
			return &queryError{"invalidArguments", "conditions must be an array"}
		}
		for _, c := range conds {
			if c == nil {
				return &queryError{"invalidArguments", "condition must be an object"}
			}
			if err := validateEmailFilter(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for k := range m {
		if !supportedEmailFilterKeys[k] {
			return &queryError{"unsupportedFilter", "unsupported filter property " + k}
		}
	}
	// Strict typing of the supported properties.
	data, _ := json.Marshal(m)
	var cond FilterCondition
	if err := json.Unmarshal(data, &cond); err != nil {
		return &queryError{"invalidArguments", "invalid filter value: " + err.Error()}
	}
	return nil
}

// parseEmailComparators validates and decodes the sort argument. Unlike the
// Go zero value, isAscending defaults to true when omitted (RFC 8620 §5.5).
func parseEmailComparators(sortArg interface{}) ([]Comparator, *queryError) {
	if sortArg == nil {
		return nil, nil
	}
	list, ok := sortArg.([]interface{})
	if !ok {
		return nil, &queryError{"invalidArguments", "sort must be an array"}
	}
	comps := make([]Comparator, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, &queryError{"invalidArguments", "comparator must be an object"}
		}
		prop, ok := m["property"].(string)
		if !ok || prop == "" {
			return nil, &queryError{"invalidArguments", "comparator property must be a string"}
		}
		if !supportedEmailSortProps[prop] {
			return nil, &queryError{"unsupportedSort", "unsupported sort property " + prop}
		}
		asc := true
		if v, has := m["isAscending"]; has && v != nil {
			b, ok := v.(bool)
			if !ok {
				return nil, &queryError{"invalidArguments", "isAscending must be a boolean"}
			}
			asc = b
		}
		comps = append(comps, Comparator{Property: prop, IsAscending: asc})
	}
	return comps, nil
}

// evalEmailFilter evaluates a validated filter against one Email, whose
// mailbox membership is boxes (RFC 8620 §5.5: AND, OR and NOT = none match).
func evalEmailFilter(node interface{}, meta *storage.MessageMetadata, boxes map[string]bool) bool {
	m, ok := node.(map[string]interface{})
	if !ok {
		return true
	}
	if op, has := m["operator"].(string); has {
		conds, _ := m["conditions"].([]interface{})
		switch op {
		case "AND":
			for _, c := range conds {
				if !evalEmailFilter(c, meta, boxes) {
					return false
				}
			}
			return true
		default: // OR, NOT
			for _, c := range conds {
				if evalEmailFilter(c, meta, boxes) {
					return op == "OR"
				}
			}
			return op == "NOT"
		}
	}
	cond := parseFilter(m)
	if !matchesFilter(meta, cond) {
		return false
	}
	if cond.InMailbox != "" && !boxes[getMailboxNameFromID(cond.InMailbox)] {
		return false
	}
	if len(cond.InMailboxOtherThan) > 0 {
		other := false
		for b := range boxes {
			if !mailboxExcluded(b, cond.InMailboxOtherThan) {
				other = true
				break
			}
		}
		if !other {
			return false
		}
	}
	return true
}

// validateQueryArgTypes enforces the argument types shared by /query methods
// (RFC 8620 §5.5): wrong types are invalidArguments, never silently defaulted.
func validateQueryArgTypes(args map[string]interface{}) *queryError {
	for _, k := range []string{"position", "limit", "anchorOffset"} {
		if v, has := args[k]; has && v != nil {
			if _, ok := v.(float64); !ok {
				return &queryError{"invalidArguments", k + " must be a number"}
			}
		}
	}
	if v, has := args["anchor"]; has && v != nil {
		if _, ok := v.(string); !ok {
			return &queryError{"invalidArguments", "anchor must be a string"}
		}
	}
	if v, has := args["calculateTotal"]; has && v != nil {
		if _, ok := v.(bool); !ok {
			return &queryError{"invalidArguments", "calculateTotal must be a boolean"}
		}
	}
	return nil
}

// validateSetArgTypes enforces /set argument types (RFC 8620 §5.3).
func validateSetArgTypes(args map[string]interface{}) *queryError {
	for _, k := range []string{"create", "update"} {
		if v, has := args[k]; has && v != nil {
			if _, ok := v.(map[string]interface{}); !ok {
				return &queryError{"invalidArguments", k + " must be an object"}
			}
		}
	}
	if v, has := args["destroy"]; has && v != nil {
		if err := validateStringArray(v, "destroy"); err != nil {
			return err
		}
	}
	if v, has := args["ifInState"]; has && v != nil {
		if _, ok := v.(string); !ok {
			return &queryError{"invalidArguments", "ifInState must be a string"}
		}
	}
	return nil
}

func validateStringArray(v interface{}, name string) *queryError {
	arr, ok := v.([]interface{})
	if !ok {
		return &queryError{"invalidArguments", name + " must be an array of strings"}
	}
	for _, e := range arr {
		if _, ok := e.(string); !ok {
			return &queryError{"invalidArguments", name + " must contain only strings"}
		}
	}
	return nil
}

// validKeywordSyntax reports whether kw satisfies the RFC 8621 §2 keyword
// syntax: 1-255 characters of %x21-%x7e, excluding ( ) { ] % * " \. The
// check runs on the RAW name; callers lowercase accepted keywords at the
// write boundary (§2: servers MUST return keywords in lowercase).
func validKeywordSyntax(kw string) bool {
	if len(kw) < 1 || len(kw) > 255 {
		return false
	}
	for i := 0; i < len(kw); i++ {
		c := kw[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
		switch c {
		case '(', ')', '{', ']', '%', '*', '"', '\\':
			return false
		}
	}
	return true
}

// keywordModeled reports whether the server can persist keyword kw. The
// server stores ANY syntactically valid RFC 8621 §2 keyword (lowercased at
// the write boundary), so "modeled" means syntax-valid; the four system
// keywords additionally map to IMAP system flags via jmapKeywordToIMAPFlag.
func keywordModeled(kw string) bool {
	return validKeywordSyntax(kw)
}
