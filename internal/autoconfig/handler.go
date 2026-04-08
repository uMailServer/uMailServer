package autoconfig

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
)

// DomainGetter interface for looking up domain configuration
type DomainGetter interface {
	GetDomain(name string) (interface{}, error)
}

// ConfigProvider provides email server configuration
type ConfigProvider interface {
	GetMailServerHost(domain string) string
	GetIncomingPort(domain string, protocol string, ssl bool) int
	GetOutgoingPort(domain string, ssl bool) int
	SupportsSSL(domain string) bool
}

// DefaultConfigProvider provides default configuration
type DefaultConfigProvider struct{}

// GetMailServerHost returns the mail server hostname for a domain
func (p *DefaultConfigProvider) GetMailServerHost(domain string) string {
	return "mail." + domain
}

// GetIncomingPort returns the incoming server port
func (p *DefaultConfigProvider) GetIncomingPort(domain string, protocol string, ssl bool) int {
	ports := DefaultPorts()
	if proto, ok := ports[protocol]; ok {
		if ssl {
			return proto["SSL"]
		}
		return proto["STARTTLS"]
	}
	return 993 // Default IMAP SSL
}

// GetOutgoingPort returns the outgoing server port
func (p *DefaultConfigProvider) GetOutgoingPort(domain string, ssl bool) int {
	if ssl {
		return 465
	}
	return 587
}

// SupportsSSL returns whether SSL is supported
func (p *DefaultConfigProvider) SupportsSSL(domain string) bool {
	return true
}

// Handler handles autoconfig HTTP requests
type Handler struct {
	provider  ConfigProvider
	validator *Validator
}

// NewHandler creates a new autoconfig handler
func NewHandler(provider ConfigProvider) *Handler {
	if provider == nil {
		provider = &DefaultConfigProvider{}
	}
	return &Handler{
		provider:  provider,
		validator: NewValidator(),
	}
}

// HandleAutoconfig handles Mozilla-style autoconfig requests
// Path: /.well-known/autoconfig/mail/config-v1.1.xml
func (h *Handler) HandleAutoconfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	domain := h.extractDomain(r)
	if domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}

	if err := h.validator.ValidateEmail("test@" + domain); err != nil {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}

	config := h.buildAutoconfig(domain)

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	xml.NewEncoder(w).Encode(config)
}

// HandleAutodiscover handles Microsoft Autodiscover requests
// Path: /autodiscover/autodiscover.xml
func (h *Handler) HandleAutodiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var email string

	if r.Method == http.MethodGet {
		email = r.URL.Query().Get("email")
		if email == "" {
			email = h.extractEmailFromHost(r.Host)
		}
	} else {
		// POST - read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		email = h.parseEmailFromXML(body)
		if email == "" {
			email = r.URL.Query().Get("email")
		}
	}

	if email == "" {
		http.Error(w, "Email address required", http.StatusBadRequest)
		return
	}

	if err := h.validator.ValidateEmail(email); err != nil {
		http.Error(w, "Invalid email", http.StatusBadRequest)
		return
	}

	domain := h.validator.ExtractDomain(email)
	if domain == "" {
		http.Error(w, "Invalid email domain", http.StatusBadRequest)
		return
	}

	resp := h.buildAutodiscoverResponse(email, domain)

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	xml.NewEncoder(w).Encode(resp)
}

func (h *Handler) extractDomain(r *http.Request) string {
	host := r.Host
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}

	// Check if it's autoconfig subdomain
	if strings.HasPrefix(host, "autoconfig.") {
		domain := strings.TrimPrefix(host, "autoconfig.")
		if h.validator.IsValidDomain(domain) {
			return domain
		}
	}

	// Check if it's autodiscover subdomain
	if strings.HasPrefix(host, "autodiscover.") {
		domain := strings.TrimPrefix(host, "autodiscover.")
		if h.validator.IsValidDomain(domain) {
			return domain
		}
	}

	// Check if host looks like a domain
	if h.validator.IsValidDomain(host) {
		return host
	}

	return ""
}

func (h *Handler) extractEmailFromHost(host string) string {
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}
	if strings.Contains(host, "@") {
		return strings.ToLower(host)
	}
	return ""
}

func (h *Handler) parseEmailFromXML(body []byte) string {
	// Try to parse as Autodiscover request
	var req AutodiscoverRequest
	if err := xml.Unmarshal(body, &req); err == nil {
		if req.Request.EMailAddress != "" {
			return req.Request.EMailAddress
		}
	}
	return ""
}

func (h *Handler) buildAutoconfig(domain string) *AutoconfigClientConfig {
	ssl := h.provider.SupportsSSL(domain)
	hostname := h.provider.GetMailServerHost(domain)

	port := h.provider.GetIncomingPort(domain, "imap", ssl)
	outPort := h.provider.GetOutgoingPort(domain, ssl)

	socketType := "SSL"
	if !ssl {
		socketType = "STARTTLS"
	}

	return &AutoconfigClientConfig{
		Version: "1.1",
		Providers: []AutoconfigProvider{
			{
				ID:          domain,
				Domain:      []string{domain},
				DisplayName: domain,
				IncomingServers: []AutoconfigServer{
					{
						Type:           "imap",
						Hostname:       hostname,
						Port:           port,
						SocketType:     socketType,
						Username:       "%EMAILADDRESS%",
						Authentication: h.getAuthMethod(ssl),
					},
				},
				OutgoingServers: []AutoconfigServer{
					{
						Type:           "smtp",
						Hostname:       hostname,
						Port:           outPort,
						SocketType:     socketType,
						Username:       "%EMAILADDRESS%",
						Authentication: h.getAuthMethod(ssl),
					},
				},
			},
		},
	}
}

func (h *Handler) getAuthMethod(ssl bool) string {
	if ssl {
		return "password-encrypted"
	}
	return "password-cleartext"
}

func (h *Handler) buildAutodiscoverResponse(email, domain string) *AutodiscoverResponse {
	hostname := h.provider.GetMailServerHost(domain)

	resp := &AutodiscoverResponse{
		Space: "http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006",
	}

	resp.Response.User.DisplayName = email
	resp.Response.User.EMailAddress = email
	resp.Response.Account.AccountType = "email"
	resp.Response.Account.Action = "settings"

	resp.Response.Account.Protocol = []AutodiscoverProtocol{
		{
			Type:      "IMAP",
			Server:    hostname,
			Port:      993,
			LoginName: email,
			Domain:    domain,
			SPA:       "off",
			SSL:       "on",
			Auth:      "password-encrypted",
		},
		{
			Type:      "SMTP",
			Server:    hostname,
			Port:      465,
			LoginName: email,
			Domain:    domain,
			SPA:       "off",
			SSL:       "on",
			Auth:      "password-encrypted",
		},
	}

	return resp
}
