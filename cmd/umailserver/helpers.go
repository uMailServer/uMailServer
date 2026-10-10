package main

import (
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/umailserver/umailserver/internal/config"
)

// dkimPublicKeyB64 extracts the base64 DER body (the DNS "p=" value) from a
// PEM-armored PKIX public key. The armor lines and newlines must never reach a
// DNS TXT record (F6210).
func dkimPublicKeyB64(pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in public key")
	}
	return strings.TrimSpace(pemBodyB64(block.Bytes)), nil
}

func pemBodyB64(der []byte) string {
	return base64.StdEncoding.EncodeToString(der)
}

// txtRecordValue renders a TXT value as one or more quoted character-strings of
// at most 255 bytes each (RFC 1035 3.3.14); a 2048-bit DKIM key exceeds the
// limit and a single string is rejected by DNS providers (F6210).
func txtRecordValue(value string) string {
	const max = 255
	if len(value) <= max {
		return `"` + value + `"`
	}
	var parts []string
	for len(value) > max {
		parts = append(parts, `"`+value[:max]+`"`)
		value = value[max:]
	}
	if value != "" {
		parts = append(parts, `"`+value+`"`)
	}
	return strings.Join(parts, " ")
}

// dkimTXT builds the full DKIM TXT value for a base64 public key.
func dkimTXT(pubB64 string) string {
	return txtRecordValue("v=DKIM1; k=rsa; p=" + pubB64)
}

// writeSecretFile writes data with the given mode, enforcing the mode even if
// the file already exists with looser permissions (F6211).
func writeSecretFile(path string, data []byte, mode os.FileMode) error {
	path = filepath.Clean(path)
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// parseEmail splits local@domain, requiring both parts to be non-empty.
func parseEmail(email string) (local, domain string, ok bool) {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(email, " \t\r\n/:") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// requireConfigFile returns an error when an explicitly named config file does
// not exist. config.Load silently falls back to defaults for a missing file, so
// a typo in --config would start the server (or back up) with wrong settings.
func requireConfigFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	return nil
}

// configSearchPaths lists where subcommands look for a config file when none
// is given explicitly: $UMAILSERVER_CONFIG, the working directory, and the
// location quickstart writes to.
func configSearchPaths() []string {
	var paths []string
	if p := os.Getenv("UMAILSERVER_CONFIG"); p != "" {
		paths = append(paths, p)
	}
	return append(paths, "./umailserver.yaml", "./umailserver.yml", "./demo.yaml", "/etc/umailserver/umailserver.yaml")
}

// dnsRecordLines renders the MX/A/SPF/DKIM/DMARC records for a domain from
// stored data. selector/pubB64 come from the domain record; when no key is
// stored an explicit notice is printed instead of a fake record (F6310).
func dnsRecordLines(domain, selector, pubB64 string) string {
	if selector == "" {
		selector = "default"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# MX Record:\n%s.    IN    MX    10    mail.%s.\n\n", domain, domain)
	fmt.Fprintf(&b, "# A Record:\nmail.%s.    IN    A    <YOUR_SERVER_IP>\n\n", domain)
	fmt.Fprintf(&b, "# SPF Record:\n%s.    IN    TXT    \"v=spf1 mx ~all\"\n\n", domain)
	fmt.Fprintf(&b, "# DKIM Record (%s._domainkey):\n", selector)
	if pubB64 == "" {
		fmt.Fprintf(&b, "(no DKIM public key stored for this domain; re-add the domain to generate one)\n\n")
	} else {
		fmt.Fprintf(&b, "%s._domainkey.%s.    IN    TXT    %s\n\n", selector, domain, dkimTXT(pubB64))
	}
	fmt.Fprintf(&b, "# DMARC Record:\n_dmarc.%s.    IN    TXT    \"v=DMARC1; p=quarantine; rua=mailto:dmarc@%s\"\n\n", domain, domain)
	return b.String()
}

// cmdConfig implements `umailserver config check [--config path]`: it loads and
// validates the file exactly as `serve` would and additionally fails on unknown
// YAML keys (typos such as spam.thershold that Load only warns about).
// It returns the process exit code.
func cmdConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, "Usage: umailserver config check [--config path]")
		return 1
	}
	fs := flag.NewFlagSet("config check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", "", "Path to config file (default: $UMAILSERVER_CONFIG or ./umailserver.yaml)")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "config check: unexpected argument: %s\n", fs.Arg(0))
		return 1
	}
	if *path == "" {
		if p := os.Getenv("UMAILSERVER_CONFIG"); p != "" {
			*path = p
		} else {
			*path = "./umailserver.yaml"
		}
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintf(stderr, "config check: %v\n", err)
		return 1
	}
	if err := cfg.ValidateStrict(); err != nil {
		fmt.Fprintf(stderr, "config check: %s: %v\n", *path, err)
		return 1
	}
	fmt.Fprintf(stdout, "config OK: %s\n", cfg.Source())
	return 0
}
