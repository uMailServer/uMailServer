package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/umailserver/umailserver/internal/db"
)

func parseEmail(email string) (user, domain string) {
	at := strings.LastIndex(email, "@")
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// validateDomainName validates domain name format and checks for path traversal
func validateDomainName(name string) error {
	if name == "" {
		return fmt.Errorf("domain name cannot be empty")
	}
	// Check for path traversal sequences and invalid characters
	if strings.Contains(name, "..") {
		return fmt.Errorf("domain name contains invalid sequence")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("domain name contains invalid characters")
	}
	// Check length
	if len(name) > 253 {
		return fmt.Errorf("domain name exceeds maximum length")
	}
	// F6060: LDH labels only. The name becomes a DKIM DNS record owner, an
	// account-key prefix and a path component, so whitespace, control
	// characters, '@', ';' and leading/trailing hyphens must not get in.
	// Single-label domains (like "localhost") are allowed.
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("domain name has an invalid label")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("domain name contains invalid characters")
			}
		}
	}
	return nil
}

// validateEmailFormat validates email address format
func validateEmailFormat(email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	// Check for path traversal sequences and invalid characters
	if strings.Contains(email, "..") {
		return fmt.Errorf("email contains invalid sequence")
	}
	if strings.ContainsAny(email, "/\\\r\n\x00") {
		return fmt.Errorf("email contains invalid characters")
	}
	// Must have exactly one @
	at := strings.Count(email, "@")
	if at != 1 {
		return fmt.Errorf("email must contain exactly one @ character")
	}
	user, domain := parseEmail(email)
	if user == "" || domain == "" {
		return fmt.Errorf("email format is invalid")
	}
	if len(user) > 64 {
		return fmt.Errorf("email local part exceeds maximum length")
	}
	if len(domain) > 253 {
		return fmt.Errorf("email domain exceeds maximum length")
	}
	return nil
}

// validatePassword checks password strength
func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > maxPasswordLength {
		return fmt.Errorf("password exceeds maximum length of %d characters", maxPasswordLength)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case strings.ContainsRune("!@#$%^&*()_+-=[]{}|;':\",./<>?`~\\", c):
			hasSpecial = true
		}
	}
	if !hasUpper {
		return fmt.Errorf("password must contain at least one uppercase letter")
	}
	if !hasLower {
		return fmt.Errorf("password must contain at least one lowercase letter")
	}
	if !hasDigit {
		return fmt.Errorf("password must contain at least one digit")
	}
	if !hasSpecial {
		return fmt.Errorf("password must contain at least one special character")
	}
	return nil
}

// maxPasswordLength is the longest password any flow accepts.
const maxPasswordLength = 128

// isKeyNotFound reports whether err is db.Get's "key not found" (the db
// package returns it untyped), as opposed to a storage failure.
func isKeyNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "key not found")
}

// dbErrStatus maps the db package's sentinel errors to an HTTP status and
// message (F6132); anything else is a 500 with the fallback message.
func dbErrStatus(err error, fallback string) (int, string) {
	switch {
	case errors.Is(err, db.ErrAccountNotFound):
		return http.StatusNotFound, "account not found"
	case errors.Is(err, db.ErrAliasNotFound):
		return http.StatusNotFound, "alias not found"
	case errors.Is(err, db.ErrDomainNotFound):
		return http.StatusNotFound, "domain not found"
	case errors.Is(err, db.ErrAccountExists):
		return http.StatusConflict, "account already exists"
	case errors.Is(err, db.ErrAliasExists):
		return http.StatusConflict, "alias already exists"
	case errors.Is(err, db.ErrDomainExists):
		return http.StatusConflict, "domain already exists"
	case errors.Is(err, db.ErrAliasConflict):
		return http.StatusConflict, "alias conflicts with an existing mailbox or targets itself"
	case errors.Is(err, db.ErrDomainAccountLimit):
		return http.StatusConflict, "domain account limit reached"
	case errors.Is(err, db.ErrInvalidName):
		return http.StatusBadRequest, "invalid name"
	}
	return http.StatusInternalServerError, fallback
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

// parsePage reads ?limit (default 100, max 1000) and ?offset. On a bad value
// it answers 400 and reports ok=false (F6254).
func (s *Server) parsePage(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	q := r.URL.Query()
	limit = defaultPageLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageLimit {
			s.sendError(w, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.sendError(w, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// pageBounds returns the [lo,hi) slice window for a list of total entries.
func pageBounds(total, limit, offset int) (lo, hi int) {
	if offset > total {
		offset = total
	}
	hi = offset + limit
	if hi > total || hi < offset {
		hi = total
	}
	return offset, hi
}

// setTotalCount exposes the unpaginated size so clients can page while the
// response body stays a plain array.
func setTotalCount(w http.ResponseWriter, total int) {
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
}
