package api

import (
	"encoding/xml"
	"net/http"
	"strings"
)

// AutoconfigProvider represents an email provider in autoconfig
type AutoconfigProvider struct {
	XMLName         xml.Name           `xml:"emailProvider"`
	ID              string             `xml:"id,attr"`
	Domain          []string           `xml:"domain"`
	IncomingServers []AutoconfigServer `xml:"incomingServer"`
	OutgoingServers []AutoconfigServer `xml:"outgoingServer"`
}

// AutoconfigServer represents a server configuration
type AutoconfigServer struct {
	Type           string `xml:"type,attr"`
	Hostname       string `xml:"hostname"`
	Port           int    `xml:"port"`
	SocketType     string `xml:"socketType"`
	Username       string `xml:"username"`
	Authentication string `xml:"authentication"`
}

// AutoconfigClientConfig is the root element
type AutoconfigClientConfig struct {
	XMLName   xml.Name             `xml:"clientConfig"`
	Version   string               `xml:"version,attr"`
	Providers []AutoconfigProvider `xml:"emailProvider"`
}

// handleAutoconfig handles Mozilla-style autoconfig requests
// Path: /.well-known/autoconfig/mail/config-v1.1.xml
func (s *Server) handleAutoconfig(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		s.sendAutoconfigError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract domain from request
	domain := extractDomainFromRequest(r)
	if domain == "" {
		s.sendAutoconfigError(w, http.StatusBadRequest, "Domain required")
		return
	}

	// Build autoconfig response
	config := s.buildAutoconfig(domain)

	// Set headers
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Write response
	xml.NewEncoder(w).Encode(config)
}

// buildAutoconfig builds the autoconfig for a domain
func (s *Server) buildAutoconfig(domain string) *AutoconfigClientConfig {
	// Get domain config from database
	var incomingPort, outgoingPort int
	var incomingSSL, outgoingSSL bool
	var incomingType, outgoingType string

	// Default configuration
	incomingPort = 993
	outgoingPort = 465
	incomingSSL = true
	outgoingSSL = true
	incomingType = "imap"
	outgoingType = "smtp"

	// Try to get domain-specific settings from config
	if s.db != nil {
		if d, err := s.db.GetDomain(domain); err == nil && d != nil {
			// Use domain-specific settings if available
			// For now, use defaults
		}
	}

	config := &AutoconfigClientConfig{
		Version: "1.1",
		Providers: []AutoconfigProvider{
			{
				ID:     domain,
				Domain: []string{domain},
				IncomingServers: []AutoconfigServer{
					{
						Type:           incomingType,
						Hostname:       getMailServer(domain),
						Port:           incomingPort,
						SocketType:     getSocketType(incomingSSL),
						Username:       "%EMAILADDRESS%",
						Authentication: getAuthMethod(incomingSSL),
					},
				},
				OutgoingServers: []AutoconfigServer{
					{
						Type:           outgoingType,
						Hostname:       getMailServer(domain),
						Port:           outgoingPort,
						SocketType:     getSocketType(outgoingSSL),
						Username:       "%EMAILADDRESS%",
						Authentication: getAuthMethod(outgoingSSL),
					},
				},
			},
		},
	}

	return config
}

// getMailServer returns the mail server hostname for a domain
func getMailServer(domain string) string {
	// Use the domain itself as the mail server if no specific config
	// In production, this could look up MX records or use a specific mail host
	return "mail." + domain
}

// getSocketType returns the socket type string based on SSL setting
func getSocketType(ssl bool) string {
	if ssl {
		return "SSL"
	}
	return "plain"
}

// getAuthMethod returns the authentication method based on SSL setting
func getAuthMethod(ssl bool) string {
	if ssl {
		return "password-encrypted"
	}
	return "password-cleartext"
}

// extractDomainFromRequest extracts the domain from the autoconfig request
func extractDomainFromRequest(r *http.Request) string {
	// Try to get domain from Host header
	host := r.Host

	// Remove port if present
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}

	// If host contains the domain, use it
	if strings.Contains(host, ".") {
		return strings.ToLower(host)
	}

	// Try to extract from the request URL path (for POST requests)
	// Path format: /autodiscover/autodiscover.xml or /.well-known/autoconfig/mail/config-v1.1.xml
	return ""
}

// sendAutoconfigError sends an XML error response for autoconfig
func (s *Server) sendAutoconfigError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(status)

	resp := struct {
		XMLName xml.Name `xml:"clientConfig"`
		Version string   `xml:"version,attr"`
		Error   struct {
			XMLName  xml.Name `xml:"error"`
			Code     int      `xml:"code,attr"`
			Message  string   `xml:"message"`
			Language string   `xml:"language,attr"`
		} `xml:"error"`
	}{
		Version: "1.1",
	}
	resp.Error.Code = status
	resp.Error.Message = message
	resp.Error.Language = "en"

	xml.NewEncoder(w).Encode(resp)
}
