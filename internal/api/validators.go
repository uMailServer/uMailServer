package api

import (
	"fmt"
	"strings"
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
	// Basic format check - should have at least one dot for multi-level domains
	// Single-label domains (like "localhost") are allowed but not ideal
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
	if strings.ContainsAny(email, "/\\") {
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
	if len(password) > 128 {
		return fmt.Errorf("password exceeds maximum length of 128 characters")
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
