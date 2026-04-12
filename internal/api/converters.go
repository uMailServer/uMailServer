package api

import (
	"github.com/umailserver/umailserver/internal/db"
)

func domainToJSON(d *db.DomainData) map[string]interface{} {
	result := map[string]interface{}{
		"name":         d.Name,
		"max_accounts": d.MaxAccounts,
		"is_active":    d.IsActive,
		"created_at":   d.CreatedAt,
		"updated_at":   d.UpdatedAt,
	}
	if d.DKIMSelector != "" {
		result["dkim_selector"] = d.DKIMSelector
		result["dkim_public_key"] = d.DKIMPublicKey
	}
	return result
}

func accountToJSON(a *db.AccountData) map[string]interface{} {
	result := map[string]interface{}{
		"email":             a.Email,
		"is_admin":          a.IsAdmin,
		"is_active":         a.IsActive,
		"quota_used":        a.QuotaUsed,
		"quota_limit":       a.QuotaLimit,
		"forward_to":        a.ForwardTo,
		"forward_keep_copy": a.ForwardKeepCopy,
		"created_at":        a.CreatedAt,
		"updated_at":        a.UpdatedAt,
		"last_login":        a.LastLoginAt,
	}
	if a.VacationSettings != "" {
		result["vacation_settings"] = a.VacationSettings
	}
	return result
}

func aliasToJSON(a *db.AliasData) map[string]interface{} {
	return map[string]interface{}{
		"alias":      a.Alias + "@" + a.Domain,
		"target":     a.Target,
		"domain":     a.Domain,
		"is_active":  a.IsActive,
		"created_at": a.CreatedAt,
	}
}
