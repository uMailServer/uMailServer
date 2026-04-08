package api

import (
	"encoding/xml"
	"net/http"
	"strings"
)

// AutodiscoverRequest represents an Autodiscover request
type AutodiscoverRequest struct {
	XMLName  xml.Name `xml:"Autodiscover"`
	Requests []struct {
		EMailAddress  string `xml:"EMailAddress"`
		AcceptableDst string `xml:"AcceptableDst"`
	} `xml:"Request"`
}

// AutodiscoverResponse represents an Autodiscover response
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
			XMLName     xml.Name `xml:"Account"`
			AccountType string   `xml:"AccountType"`
			Action      string   `xml:"Action"`
			Protocol    []struct {
				XMLName   xml.Name `xml:"Protocol"`
				Type      string   `xml:"Type"`
				Server    string   `xml:"Server"`
				Port      int      `xml:"Port"`
				LoginName string   `xml:"LoginName"`
				Domain    string   `xml:"Domain"`
				SPA       string   `xml:"SPA"`
				SSL       string   `xml:"SSL"`
				Auth      string   `xml:"Auth"`
			} `xml:"Protocol"`
		} `xml:"Account"`
	} `xml:"Response"`
}

// handleAutodiscover handles Microsoft Autodiscover requests
// Path: /autodiscover/autodiscover.xml
func (s *Server) handleAutodiscover(w http.ResponseWriter, r *http.Request) {
	// Only allow GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var email string

	if r.Method == http.MethodGet {
		// GET request - extract email from query string or host
		email = r.URL.Query().Get("email")
		if email == "" {
			// Try to extract from Host header
			email = extractEmailFromHost(r.Host)
		}
	} else {
		// POST request - parse XML body
		email = s.parseAutodiscoverPOST(r)
	}

	if email == "" {
		// Return redirect to the main autodiscover endpoint
		http.Error(w, "Email address required", http.StatusBadRequest)
		return
	}

	// Extract domain from email
	domain := extractDomainFromEmail(email)
	if domain == "" {
		http.Error(w, "Invalid email address", http.StatusBadRequest)
		return
	}

	// Build response
	resp := s.buildAutodiscoverResponse(email, domain)

	// Set headers
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Write response
	xml.NewEncoder(w).Encode(resp)
}

// parseAutodiscoverPOST parses the POST body for email address
func (s *Server) parseAutodiscoverPOST(r *http.Request) string {
	// For POST requests, we would normally parse the XML body
	// For now, extract email from request
	email := r.URL.Query().Get("email")
	if email == "" {
		email = extractEmailFromHost(r.Host)
	}
	return email
}

// buildAutodiscoverResponse builds the Autodiscover response
func (s *Server) buildAutodiscoverResponse(email, domain string) *AutodiscoverResponse {
	resp := &AutodiscoverResponse{
		Space: "http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006",
	}

	resp.Response.User.DisplayName = email
	resp.Response.User.EMailAddress = email
	resp.Response.Account.AccountType = "email"
	resp.Response.Account.Action = "settings"

	// Add IMAP protocol
	imapProtocol := struct {
		XMLName   xml.Name `xml:"Protocol"`
		Type      string   `xml:"Type"`
		Server    string   `xml:"Server"`
		Port      int      `xml:"Port"`
		LoginName string   `xml:"LoginName"`
		Domain    string   `xml:"Domain"`
		SPA       string   `xml:"SPA"`
		SSL       string   `xml:"SSL"`
		Auth      string   `xml:"Auth"`
	}{
		Type:      "IMAP",
		Server:    "mail." + domain,
		Port:      993,
		LoginName: email,
		Domain:    domain,
		SPA:       "off",
		SSL:       "on",
		Auth:      "password-encrypted",
	}

	// Add SMTP protocol
	smtpProtocol := struct {
		XMLName   xml.Name `xml:"Protocol"`
		Type      string   `xml:"Type"`
		Server    string   `xml:"Server"`
		Port      int      `xml:"Port"`
		LoginName string   `xml:"LoginName"`
		Domain    string   `xml:"Domain"`
		SPA       string   `xml:"SPA"`
		SSL       string   `xml:"SSL"`
		Auth      string   `xml:"Auth"`
	}{
		Type:      "SMTP",
		Server:    "mail." + domain,
		Port:      465,
		LoginName: email,
		Domain:    domain,
		SPA:       "off",
		SSL:       "on",
		Auth:      "password-encrypted",
	}

	resp.Response.Account.Protocol = append(resp.Response.Account.Protocol, imapProtocol, smtpProtocol)

	return resp
}

// extractEmailFromHost extracts email from Host header
func extractEmailFromHost(host string) string {
	// Remove port if present
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}

	// Check if host looks like an email (user@domain)
	if strings.Contains(host, "@") {
		return strings.ToLower(host)
	}

	return ""
}

// extractDomainFromEmail extracts domain from email address
func extractDomainFromEmail(email string) string {
	if idx := strings.Index(email, "@"); idx > 0 {
		return strings.ToLower(email[idx+1:])
	}
	return ""
}
