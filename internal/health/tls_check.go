package health

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// TLSCertificateCheck creates a TLS certificate expiry checker
func TLSCertificateCheck(certPath, keyPath string, warningDays, criticalDays int) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "tls_certificate",
			Status:  StatusHealthy,
			Details: make(map[string]interface{}),
		}

		// Read certificate file
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			// If cert doesn't exist (e.g., using ACME), check is skipped
			if os.IsNotExist(err) {
				check.Status = StatusHealthy
				check.Message = "TLS certificate managed by ACME (auto-renewal)"
				check.Details["path"] = certPath
				check.Details["auto_managed"] = true
				return check
			}
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to read certificate: %v", err)
			return check
		}

		// Parse certificate
		block, _ := pem.Decode(certPEM)
		if block == nil {
			check.Status = StatusUnhealthy
			check.Message = "failed to parse certificate PEM"
			return check
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to parse certificate: %v", err)
			return check
		}

		// Calculate days until expiry
		daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)

		check.Details["subject"] = cert.Subject.String()
		check.Details["issuer"] = cert.Issuer.String()
		check.Details["not_before"] = cert.NotBefore.Format(time.RFC3339)
		check.Details["not_after"] = cert.NotAfter.Format(time.RFC3339)
		check.Details["days_until_expiry"] = daysUntilExpiry
		check.Details["dns_names"] = cert.DNSNames

		if daysUntilExpiry <= 0 {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("TLS certificate EXPIRED %d days ago", -daysUntilExpiry)
		} else if daysUntilExpiry <= criticalDays {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("TLS certificate expires in %d days (critical)", daysUntilExpiry)
		} else if daysUntilExpiry <= warningDays {
			check.Status = StatusDegraded
			check.Message = fmt.Sprintf("TLS certificate expires in %d days (warning)", daysUntilExpiry)
		} else {
			check.Message = fmt.Sprintf("TLS certificate valid for %d days", daysUntilExpiry)
		}

		return check
	}
}
