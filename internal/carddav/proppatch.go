package carddav

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// propPatchOp is one DAV:set or DAV:remove entry of a PROPPATCH body.
type propPatchOp struct {
	remove bool
	name   xml.Name
	value  string
}

var errInvalidPropertyUpdate = errors.New("invalid propertyupdate")

// parsePropertyUpdate reads a DAV:propertyupdate body into its operations, in
// document order (RFC 4918 §9.2).
func parsePropertyUpdate(body []byte) ([]propPatchOp, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var ops []propPatchOp
	var path []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			path = append(path, t.Name.Local)
			switch len(path) {
			case 1:
				if t.Name.Local != "propertyupdate" {
					return nil, errInvalidPropertyUpdate
				}
			case 4:
				if path[2] == "prop" && (path[1] == "set" || path[1] == "remove") {
					ops = append(ops, propPatchOp{remove: path[1] == "remove", name: t.Name})
				}
			}
		case xml.CharData:
			if len(path) >= 4 && path[1] == "set" && len(ops) > 0 {
				ops[len(ops)-1].value += string(t)
			}
		case xml.EndElement:
			path = path[:len(path)-1]
		}
	}
	if len(ops) == 0 {
		return nil, errInvalidPropertyUpdate
	}
	return ops, nil
}

// propPatchStatus decides one operation: whether the address book supports
// the change. Only the address book's display name and description are
// writable; the other live properties are protected (403) and unknown dead
// properties cannot be stored (403 on set); removing a property that does not
// exist succeeds (RFC 4918 §9.2).
func propPatchStatus(op propPatchOp) int {
	name, desc := isWritableBookProp(op.name)
	if name || desc {
		return http.StatusOK
	}
	if !op.remove || isProtectedProp(op.name) {
		return http.StatusForbidden
	}
	return http.StatusOK
}

// isWritableBookProp reports which writable address book property a name is.
// A name without a namespace is accepted, as before.
func isWritableBookProp(n xml.Name) (displayName, description bool) {
	switch {
	case n.Local == "displayname" && (n.Space == nsDAV || n.Space == ""):
		return true, false
	case n.Local == "addressbook-description" && (n.Space == nsCardDAV || n.Space == ""):
		return false, true
	}
	return false, false
}

// isProtectedProp reports whether n is a live property the server computes.
func isProtectedProp(n xml.Name) bool {
	switch n.Space {
	case nsDAV:
		switch n.Local {
		case "resourcetype", "getetag", "getcontenttype", "getcontentlength", "getlastmodified",
			"supported-report-set", "current-user-principal", "creationdate":
			return true
		}
	case nsCardDAV:
		switch n.Local {
		case "supported-collation-set", "supported-address-data", "addressbook-home-set", "max-resource-size":
			return true
		}
	case nsCalServer:
		return n.Local == "getctag"
	}
	return false
}

// handleProppatch handles PROPPATCH on an address book (RFC 4918 §9.2). The
// request is atomic: if any operation cannot be applied, none is, the failing
// properties report their own status and the rest 424. It answers 207 with a
// propstat per status (F5665); it used to acknowledge with 200 and an empty
// body without applying anything.
func (s *Server) handleProppatch(w http.ResponseWriter, r *http.Request, username string) {
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
		return
	}
	addressbookID := parts[0]

	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusNotFound, "addressbook not found")
		return
	}
	// Contacts have no writable properties; the address book must not be
	// modified on their behalf.
	if len(parts) == 2 && parts[1] != "" {
		s.sendError(w, http.StatusForbidden, "contact properties cannot be modified")
		return
	}

	ops, err := parsePropertyUpdate(body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid propertyupdate")
		return
	}

	statuses := make([]int, len(ops))
	failed := false
	for i, op := range ops {
		statuses[i] = propPatchStatus(op)
		if statuses[i] != http.StatusOK {
			failed = true
		}
	}
	if failed {
		for i := range statuses {
			if statuses[i] == http.StatusOK {
				statuses[i] = http.StatusFailedDependency
			}
		}
	} else if !s.applyPropertyUpdate(w, username, addressbookID, ops) {
		return
	}

	s.writeMultistatus(w, &Multistatus{Responses: []Response{{
		Href:     r.URL.Path,
		Propstat: propPatchPropstats(ops, statuses),
	}}}, nil)
}

// applyPropertyUpdate applies the operations to the stored address book under
// writeMu so concurrent PROPPATCH requests do not lose each other's updates.
// It writes the error response and returns false on failure.
func (s *Server) applyPropertyUpdate(w http.ResponseWriter, username, addressbookID string, ops []propPatchOp) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusNotFound, "addressbook not found")
		return false
	}
	for _, op := range ops {
		isName, isDesc := isWritableBookProp(op.name)
		switch {
		case isName && op.remove:
			ab.Name = ab.ID
		case isName:
			ab.Name = op.value
		case isDesc && op.remove:
			ab.Description = ""
		case isDesc:
			ab.Description = op.value
		}
	}
	if err := s.storage.UpdateAddressbook(username, ab); err != nil {
		s.logger.Error("Failed to update addressbook", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to update addressbook")
		return false
	}
	return true
}

// propPatchPropstats groups the operations' property names by status, in the
// order the statuses first appear.
func propPatchPropstats(ops []propPatchOp, statuses []int) []Propstat {
	var order []int
	byStatus := map[int][]Property{}
	for i, op := range ops {
		if _, seen := byStatus[statuses[i]]; !seen {
			order = append(order, statuses[i])
		}
		byStatus[statuses[i]] = append(byStatus[statuses[i]], Property{XMLName: op.name})
	}
	out := make([]Propstat, 0, len(order))
	for _, code := range order {
		out = append(out, Propstat{
			Prop:   byStatus[code],
			Status: statusLine(code),
		})
	}
	return out
}

// statusLine formats an HTTP status line for DAV:status.
func statusLine(code int) string {
	return "HTTP/1.1 " + strconv.Itoa(code) + " " + http.StatusText(code)
}
