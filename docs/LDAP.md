# LDAP / Active Directory Integration

uMailServer supports LDAP and Active Directory (AD) authentication for enterprise environments.

## Overview

LDAP authentication allows users to log in using their existing corporate credentials. When enabled:

1. User credentials are validated against LDAP/AD
2. User details (email, name, groups) are retrieved from LDAP
3. Groups can be mapped to admin privileges
4. Local authentication is used as fallback

## Configuration

Add LDAP configuration to `umailserver.yaml`:

```yaml
ldap:
  enabled: true
  url: "ldaps://ldap.example.com:636"  # or ldap:// for non-TLS
  bind_dn: "cn=service,ou=users,dc=example,dc=com"
  bind_password: "service_account_password"
  base_dn: "ou=users,dc=example,dc=com"
  user_filter: "(uid=%s)"              # or "(sAMAccountName=%s)" for AD
  email_attribute: "mail"
  name_attribute: "cn"
  group_attribute: "memberOf"
  admin_groups:
    - "cn=mail-admins,ou=groups,dc=example,dc=com"
  start_tls: false                     # Use StartTLS on port 389
  skip_verify: false                   # Skip TLS cert verification (dev only)
  timeout: 30s
```

## Active Directory Configuration

For Microsoft Active Directory:

```yaml
ldap:
  enabled: true
  url: "ldaps://ad.example.com:636"
  bind_dn: "CN=ServiceAccount,CN=Users,DC=example,DC=com"
  bind_password: "password"
  base_dn: "CN=Users,DC=example,DC=com"
  user_filter: "(sAMAccountName=%s)"   # Windows username format
  email_attribute: "mail"
  name_attribute: "displayName"
  group_attribute: "memberOf"
  admin_groups:
    - "CN=Domain Admins,CN=Users,DC=example,DC=com"
```

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `enabled` | Enable LDAP authentication | `false` |
| `url` | LDAP server URL | Required |
| `bind_dn` | Service account DN for directory lookup | Optional |
| `bind_password` | Service account password | Optional |
| `base_dn` | Base DN for user search | Required |
| `user_filter` | Filter to find user | `(uid=%s)` |
| `email_attribute` | Attribute for email address | `mail` |
| `name_attribute` | Attribute for display name | `cn` |
| `group_attribute` | Attribute for group membership | `memberOf` |
| `admin_groups` | Groups that grant admin access | `[]` |
| `start_tls` | Use StartTLS (for ldap://) | `false` |
| `skip_verify` | Skip TLS verification | `false` |
| `timeout` | Connection timeout | `30s` |

## TLS Configuration

### LDAPS (Port 636)

```yaml
ldap:
  url: "ldaps://ldap.example.com:636"
  skip_verify: false  # Set true for self-signed certs (dev only)
```

### StartTLS (Port 389)

```yaml
ldap:
  url: "ldap://ldap.example.com:389"
  start_tls: true
```

## User Filter Examples

| Directory | Filter |
|-----------|--------|
| OpenLDAP | `(uid=%s)` |
| Active Directory | `(sAMAccountName=%s)` |
| With email | `(&(uid=%s)(mail=*))` |
| Only active users | `(&(uid=%s)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))` |

## Group Mapping

Users in specified admin groups get admin privileges:

```yaml
admin_groups:
  - "cn=mail-admins,ou=groups,dc=example,dc=com"
  - "cn=domain-admins,ou=groups,dc=example,dc=com"
```

## Authentication Flow

1. User enters username and password
2. LDAP connection established
3. Service account binds (if configured)
4. User DN is searched using `user_filter`
5. User credentials are verified via bind
6. User attributes and groups are retrieved
7. Admin status is determined from group membership
8. JWT token is issued

## Fallback Authentication

If LDAP is enabled but authentication fails:

1. Try LDAP authentication first
2. If LDAP fails, try local authentication
3. This allows gradual migration and admin access during outages

## Testing

Test LDAP connection:

```bash
# With netcat
nc -zv ldap.example.com 636

# With ldapsearch
ldapsearch -x -H ldaps://ldap.example.com:636 \
  -D "cn=admin,dc=example,dc=com" \
  -w password \
  -b "ou=users,dc=example,dc=com" \
  "(uid=testuser)"
```

## Troubleshooting

### Connection Failures

```
ldap connection failed: connection refused
```

- Check LDAP server is running
- Verify firewall rules
- Confirm port (389/636)

### Bind Failures

```
ldap bind failed: invalid credentials
```

- Verify bind_dn and bind_password
- Check DN format
- Ensure service account is not locked

### User Not Found

```
user not found
```

- Verify base_dn is correct
- Check user_filter syntax
- Ensure user exists in directory

### TLS Errors

```
tls: failed to verify certificate
```

- Check server certificate
- Verify CA trust
- Set `skip_verify: true` for testing only

## OpenLDAP Setup Example

```bash
# Install OpenLDAP
sudo apt-get install slapd ldap-utils

# Create base structure
ldapadd -x -D "cn=admin,dc=example,dc=com" -w password <<EOF
dn: ou=users,dc=example,dc=com
objectClass: organizationalUnit
ou: users

dn: ou=groups,dc=example,dc=com
objectClass: organizationalUnit
ou: groups

dn: cn=mail-admins,ou=groups,dc=example,dc=com
objectClass: groupOfNames
cn: mail-admins
member: cn=admin,ou=users,dc=example,dc=com

dn: uid=john,ou=users,dc=example,dc=com
objectClass: inetOrgPerson
uid: john
cn: John Doe
mail: john@example.com
userPassword: {SSHA}encryptedpassword
EOF
```

## Security Best Practices

1. **Use LDAPS** - Always use TLS in production
2. **Service Account** - Use dedicated read-only service account
3. **Minimal Permissions** - Service account only needs read access
4. **Secure Password** - Store bind_password securely (env var or secret)
5. **Firewall** - Restrict LDAP server access
6. **Certificate Validation** - Don't use `skip_verify` in production

## Migration from Local Auth

To migrate existing users:

1. Enable LDAP with fallback
2. Users can login with either method
3. Gradually migrate passwords
4. Disable local auth when ready

## See Also

- [Security Hardening](SECURITY_HARDENING.md)
- [Configuration Guide](configuration.md)
- [Troubleshooting](troubleshooting.md)
