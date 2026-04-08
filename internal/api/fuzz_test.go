package api

import (
	"testing"
)

// Fuzz validateEmailFormat to find edge cases
func FuzzValidateEmailFormat(f *testing.F) {
	// Seed corpus
	validEmails := []string{
		"user@example.com",
		"user.name@example.com",
		"user+tag@example.com",
		"user@subdomain.example.com",
		"a@b.co",
	}
	for _, email := range validEmails {
		f.Add(email)
	}

	f.Fuzz(func(t *testing.T, email string) {
		err := validateEmailFormat(email)
		// We don't assert err == nil because some inputs will be invalid
		// Just verify it doesn't panic
		_ = err
	})
}

// Fuzz validateDomainName to find edge cases
func FuzzValidateDomainName(f *testing.F) {
	// Seed corpus
	validDomains := []string{
		"example.com",
		"sub.example.com",
		"x.co",
		"a123.b456.c789.com",
	}
	for _, domain := range validDomains {
		f.Add(domain)
	}

	f.Fuzz(func(t *testing.T, domain string) {
		err := validateDomainName(domain)
		_ = err
	})
}

// Fuzz validatePassword to find edge cases
func FuzzValidatePassword(f *testing.F) {
	// Seed corpus
	passwords := []string{
		"password123",
		"short",
		"",
		"verylongpasswordthatexceedsnormalexpectationsbutmightstillbevalidinput",
		"pass\000word", // with null byte
	}
	for _, pw := range passwords {
		f.Add(pw)
	}

	f.Fuzz(func(t *testing.T, password string) {
		err := validatePassword(password)
		_ = err
	})
}
