package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// legacyDatabasePath is where releases before R148 put the account database
// when database.path was left at its built-in default. An existing file there
// keeps being used so upgrades never "lose" the accounts.
const legacyDatabasePath = "/var/lib/umailserver/db"

// Source describes where the configuration came from: the config file path or
// "built-in defaults" when no file was read.
func (c *Config) Source() string {
	if c.source == "" {
		return "built-in defaults"
	}
	return c.source
}

// UnknownKeys returns the YAML keys present in the config file that match no
// known setting (for example a typo such as "spam.thershold"), each with its
// line number. Load does not fail on them for backward compatibility; use
// ValidateStrict to turn them into an error.
func (c *Config) UnknownKeys() []string {
	return append([]string(nil), c.unknownKeys...)
}

// ValidateStrict runs Validate and additionally fails on unknown YAML keys.
func (c *Config) ValidateStrict() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if len(c.unknownKeys) > 0 {
		return fmt.Errorf("unknown config keys: %s", strings.Join(c.unknownKeys, ", "))
	}
	return nil
}

// LoadOptional is Load for callers that want built-in defaults (plus
// environment overrides) when the file does not exist, e.g. diagnostics that
// probe the implicit default location. Load itself treats a missing file as an
// error because an explicitly requested path must exist.
func LoadOptional(path string) (*Config, error) {
	cfg, _, err := loadWithOptions(path, true)
	return cfg, err
}

// findUnknownKeys walks a parsed YAML node against the Config type and reports
// keys that map to no struct field.
func findUnknownKeys(n *yaml.Node, t reflect.Type, prefix string, out *[]string) {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n == nil {
		return
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			findUnknownKeys(c, t, prefix, out)
		}
	case yaml.SequenceNode:
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			for i, c := range n.Content {
				findUnknownKeys(c, t.Elem(), fmt.Sprintf("%s[%d]", strings.TrimSuffix(prefix, "."), i)+".", out)
			}
		}
	case yaml.MappingNode:
		if t.Kind() != reflect.Struct {
			return
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = strings.ToLower(f.Name)
			}
			fields[name] = f.Type
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Value == "<<" {
				continue // merge key; resolved by the decoder
			}
			ft, ok := fields[k.Value]
			if !ok {
				*out = append(*out, fmt.Sprintf("%s%s (line %d)", prefix, k.Value, k.Line))
				continue
			}
			findUnknownKeys(n.Content[i+1], ft, prefix+k.Value+".", out)
		}
	}
}

// resolvePaths rewrites relative file/directory settings that the config file
// itself set (values differing from the defaults) so they are relative to the
// directory containing the config file, not the process working directory.
func resolvePaths(cfg, defaults *Config, configPath string) {
	base, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return
	}
	fix := func(dst, def *string, isFile bool) {
		v := *dst
		if v == "" || v == *def || filepath.IsAbs(v) {
			return
		}
		if !isFile && (v == "stdout" || v == "stderr") {
			return
		}
		*dst = filepath.Join(base, v)
	}
	fix(&cfg.Server.DataDir, &defaults.Server.DataDir, true)
	fix(&cfg.TLS.CertFile, &defaults.TLS.CertFile, true)
	fix(&cfg.TLS.KeyFile, &defaults.TLS.KeyFile, true)
	fix(&cfg.TLS.ClientAuth.CAFile, &defaults.TLS.ClientAuth.CAFile, true)
	fix(&cfg.Security.AuditLog.Path, &defaults.Security.AuditLog.Path, true)
	fix(&cfg.Database.Path, &defaults.Database.Path, true)
	fix(&cfg.LDAP.RootCA, &defaults.LDAP.RootCA, true)
	// logging.output is "stdout", "stderr" or a file path.
	if o := cfg.Logging.Output; o != "" && o != "stdout" && o != "stderr" && o != defaults.Logging.Output {
		fix(&cfg.Logging.Output, &defaults.Logging.Output, true)
	}
}

// applyDatabaseDefault keeps an existing database at the pre-R148 default
// location in use. When database.path is unset and no such file exists the
// path stays empty and DatabasePath() resolves to <data_dir>/umailserver.db,
// the location every CLI subcommand already uses.
func applyDatabaseDefault(cfg *Config) {
	if cfg.Database.Path != "" {
		return
	}
	if st, err := os.Stat(legacyDatabasePath); err == nil && st.Mode().IsRegular() {
		cfg.Database.Path = legacyDatabasePath
		slog.Info("config: using existing database at legacy default location; set database.path explicitly or move it to <data_dir>/umailserver.db",
			"path", legacyDatabasePath)
	}
}

// parseDurationStrict parses a duration setting. A bare number is rejected:
// whether it means nanoseconds or seconds is a trap (30 silently became 30ns).
func parseDurationStrict(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use a unit such as 30s, 5m or 1h", s)
	}
	return d, nil
}

// validateByReflection checks, for every field of the config, the properties
// that are uniform across the whole tree: sizes and durations must not be
// negative, port settings must be within 1-65535 (0 = unset) and bind
// settings must be an IP address or host name usable as "<bind>:<port>".
func validateByReflection(v reflect.Value, path string) error {
	t := v.Type()
	switch t.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return validateByReflection(v.Elem(), path)
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := validateByReflection(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
	default:
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		p := name
		if path != "" {
			p = path + "." + name
		}
		fv := v.Field(i)
		switch {
		case f.Type == reflect.TypeOf(Size(0)):
			if fv.Int() < 0 {
				return fmt.Errorf("%s must not be negative", p)
			}
		case f.Type == reflect.TypeOf(Duration(0)) || f.Type == reflect.TypeOf(time.Duration(0)):
			if fv.Int() < 0 {
				return fmt.Errorf("%s must not be negative", p)
			}
		case f.Type.Kind() == reflect.Int && (name == "port" || strings.HasSuffix(name, "_port")):
			if n := fv.Int(); n < 0 || n > 65535 {
				return fmt.Errorf("%s %d is out of range (0-65535)", p, n)
			}
		case f.Type.Kind() == reflect.String && name == "bind":
			if err := validateBind(fv.String()); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
		default:
			if err := validateByReflection(fv, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBind accepts "", an IPv4 address, a bracketed IPv6 address ("[::1]",
// required because listeners are built as "<bind>:<port>") or a host name.
func validateBind(b string) error {
	if b == "" {
		return nil
	}
	if strings.HasPrefix(b, "[") && strings.HasSuffix(b, "]") {
		if ip := net.ParseIP(b[1 : len(b)-1]); ip == nil || ip.To4() != nil {
			return fmt.Errorf("invalid IPv6 address %q", b)
		}
		return nil
	}
	if ip := net.ParseIP(b); ip != nil {
		if ip.To4() == nil {
			return fmt.Errorf("IPv6 address %q must be written in brackets, e.g. [%s]", b, b)
		}
		return nil
	}
	if strings.ContainsAny(b, ": \t/") {
		return fmt.Errorf("invalid bind address %q: give an IP address or host name without a port", b)
	}
	if len(b) > 253 {
		return fmt.Errorf("bind host name too long")
	}
	for _, label := range strings.Split(strings.TrimSuffix(b, "."), ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("invalid bind host name %q", b)
		}
		for _, r := range label {
			if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return fmt.Errorf("invalid bind host name %q", b)
			}
		}
	}
	return nil
}
