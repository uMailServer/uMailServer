// Package autoconfig implements email client automatic configuration protocols
// RFC Reference: Mozilla Autoconfig (https://wiki.mozilla.org/Thunderbird:Autoconfiguration)
// Microsoft Autodiscover (MS-XLD)
package autoconfig

import (
	"encoding/xml"
	"fmt"
	"net/mail"
	"strings"
)

// ConfigSource represents the source of email configuration
type ConfigSource int

const (
	SourceUnknown ConfigSource = iota
	SourceAutoconfig
	SourceAutodiscover
	SourceMX
)

// EmailConfig holds complete email client configuration
type EmailConfig struct {
	Identity struct {
		EmailAddress string
		DisplayName  string
		Organization string
	}
	Incoming struct {
		Protocol       string // imap, pop3
		Hostname       string
		Port           int
		SocketType     string // SSL, STARTTLS, plain
		Username       string
		Authentication string // password-encrypted, password-cleartext, oauth2
	}
	Outgoing struct {
		Protocol       string // smtp
		Hostname       string
		Port           int
		SocketType     string // SSL, STARTTLS, plain
		Username       string
		Authentication string // password-encrypted, password-cleartext, oauth2
	}
	Sources []ConfigSource
}

// AutoconfigProvider represents an email provider in Mozilla-style autoconfig
type AutoconfigProvider struct {
	XMLName          xml.Name           `xml:"emailProvider"`
	ID               string             `xml:"id,attr"`
	Domain           []string           `xml:"domain"`
	DisplayName      string             `xml:"displayName"`
	DisplayShortName string             `xml:"displayShortName"`
	IncomingServers  []AutoconfigServer `xml:"incomingServer"`
	OutgoingServers  []AutoconfigServer `xml:"outgoingServer"`
}

// AutoconfigServer represents a server configuration in autoconfig
type AutoconfigServer struct {
	Type            string `xml:"type,attr"`
	Hostname        string `xml:"hostname"`
	Port            int    `xml:"port"`
	SocketType      string `xml:"socketType"`
	Username        string `xml:"username"`
	Authentication  string `xml:"authentication"`
	UsernameField   string `xml:"usernameField"`
	PortField       string `xml:"portField"`
	SocketTypeField string `xml:"socketTypeField"`
}

// AutoconfigClientConfig is the root element of Mozilla autoconfig
type AutoconfigClientConfig struct {
	XMLName   xml.Name             `xml:"clientConfig"`
	Version   string               `xml:"version,attr"`
	Providers []AutoconfigProvider `xml:"emailProvider"`
}

// AutodiscoverRequest represents an Autodiscover request (MS-XLD)
type AutodiscoverRequest struct {
	XMLName xml.Name `xml:"Autodiscover"`
	Space   string   `xml:"xmlns,attr"`
	Request struct {
		EMailAddress  string `xml:"EMailAddress"`
		AcceptableDst string `xml:"AcceptableDst"`
	} `xml:"Request"`
}

// AutodiscoverResponse represents an Autodiscover response (MS-XLD)
type AutodiscoverResponse struct {
	XMLName  xml.Name `xml:"AutodiscoverResponse"`
	Space    string   `xml:"xmlns,attr"`
	Response struct {
		XMLName xml.Name `xml:"Response"`
		User    struct {
			XMLName      xml.Name `xml:"User"`
			DisplayName  string   `xml:"DisplayName"`
			EMailAddress string   `xml:"EMailAddress"`
		} `xml:"User"`
		Account struct {
			XMLName     xml.Name               `xml:"Account"`
			AccountType string                 `xml:"AccountType"`
			Action      string                 `xml:"Action"`
			Protocol    []AutodiscoverProtocol `xml:"Protocol"`
		} `xml:"Account"`
	} `xml:"Response"`
}

// AutodiscoverProtocol represents a protocol configuration in Autodiscover
type AutodiscoverProtocol struct {
	XMLName   xml.Name `xml:"Protocol"`
	Type      string   `xml:"Type"`
	Server    string   `xml:"Server"`
	Port      int      `xml:"Port"`
	LoginName string   `xml:"LoginName"`
	Domain    string   `xml:"Domain"`
	SPA       string   `xml:"SPA"`
	SSL       string   `xml:"SSL"`
	Auth      string   `xml:"Auth"`
}

// Validator validates email addresses and domains
type Validator struct{}

// NewValidator creates a new autoconfig validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateEmail validates an email address
func (v *Validator) ValidateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid email address: %w", err)
	}
	return nil
}

// ExtractDomain extracts domain from email address
func (v *Validator) ExtractDomain(email string) string {
	if idx := strings.Index(email, "@"); idx > 0 {
		return strings.ToLower(email[idx+1:])
	}
	return ""
}

// IsValidDomain checks if domain looks valid
func (v *Validator) IsValidDomain(domain string) bool {
	if domain == "" {
		return false
	}
	if strings.Contains(domain, "..") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	// Basic domain validation
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 63 {
			return false
		}
	}
	return true
}

// BuildAutoconfigURL builds the Mozilla autoconfig URL for a domain
func BuildAutoconfigURL(domain string) string {
	return fmt.Sprintf("https://%s/.well-known/autoconfig/mail/config-v1.1.xml", domain)
}

// BuildAutodiscoverURL builds the Microsoft autodiscover URL for a domain
func BuildAutodiscoverURL(domain string) string {
	return fmt.Sprintf("https://autodiscover.%s/autodiscover/autodiscover.xml", domain)
}

// DefaultPorts returns default ports for email protocols
func DefaultPorts() map[string]map[string]int {
	return map[string]map[string]int{
		"imap": {
			"SSL":      993,
			"STARTTLS": 143,
			"plain":    143,
		},
		"pop3": {
			"SSL":      995,
			"STARTTLS": 110,
			"plain":    110,
		},
		"smtp": {
			"SSL":      465,
			"STARTTLS": 587,
			"plain":    25,
		},
	}
}
