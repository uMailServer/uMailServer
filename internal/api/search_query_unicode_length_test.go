package api

import (
	"net/url"
	"strings"
	"testing"
)

// F4815 retains the state/error boundary covered by the audit proof.
func TestSearchQueryLengthCountsUnicodeCharacters(t *testing.T) {
	s, _ := apiUpdateFixtureServer(t)
	rec := apiUpdateFixtureCall(t, "GET", "/api/v1/search?q="+url.QueryEscape(strings.Repeat("a", 500)), "", s.handleSearch)
	apiUpdateFixtureWant(t, "CONTROL 500 ASCII characters reach service", rec.Code, 503)
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/search?q="+url.QueryEscape(strings.Repeat("界", 500)), "", s.handleSearch)
	apiUpdateFixtureWant(t, "500 Unicode characters reach service", rec.Code, 503)
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/search?q="+url.QueryEscape(strings.Repeat("界", 501)), "", s.handleSearch)
	apiUpdateFixtureWant(t, "501 characters rejected", rec.Code, 400)
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/search?q="+url.QueryEscape("é"), "", s.handleSearch)
	apiUpdateFixtureWant(t, "single Unicode character", rec.Code, 503)
}
