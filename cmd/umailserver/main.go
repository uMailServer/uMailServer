package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/cli"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/db/migrations"
	"github.com/umailserver/umailserver/internal/server"
	"github.com/umailserver/umailserver/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

var (
	Version   = "dev"
	BuildDate = "unknown"
	GitCommit = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "quickstart":
		cmdQuickstart(os.Args[2:])
	case "domain":
		cmdDomain(os.Args[2:])
	case "account":
		cmdAccount(os.Args[2:])
	case "queue":
		cmdQueue(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "test":
		cmdTest(os.Args[2:])
	case "backup":
		cmdBackup(os.Args[2:])
	case "restore":
		cmdRestore(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "db":
		cmdDB(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "stop":
		cmdStop(os.Args[2:])
	case "restart":
		cmdRestart(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("uMailServer %s (%s) built %s\n", Version, GitCommit, BuildDate)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage(os.Stderr)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `uMailServer - One binary. Complete email.

Usage: umailserver <command> [flags]

Commands:
  serve        Start the mail server
  stop         Stop the running server
  restart      Restart the server
  status       Show server status
  quickstart   Generate config and create first account
  domain       Domain management (add, list, dns, delete)
  account      Account management (add, password, list, delete)
  queue        Queue management (list, retry, flush, drop)
  check        Diagnostics (dns, tls, deliverability)
  backup       Create backup
  restore      Restore from backup
  migrate      Import from other mail servers
  db           Database management (status, migrate, rollback)
  version      Show version
  help         Show this help

Examples:
  umailserver quickstart you@example.com
  umailserver serve --config /etc/umailserver.yaml
  umailserver status
  umailserver stop
  umailserver domain add example.com
  umailserver account add john@example.com
  umailserver check dns example.com`)
}

func cmdServe(args []string) {
	var configPath string
	var dataDir string

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.StringVar(&configPath, "config", "", "Path to config file")
	fs.StringVar(&dataDir, "data-dir", "", "Override data directory")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "serve: unexpected argument: %s\n", fs.Arg(0))
		os.Exit(1)
	}
	if err := requireConfigFile(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Without --config, look for the config the wizard/quickstart wrote in the
	// data directory (the same default the stop/status commands use) so that a
	// second run does not re-launch the first-run wizard (F6213).
	wizardDir := dataDir
	if wizardDir == "" {
		wizardDir = config.GetDefaultDataDir()
	}
	if configPath == "" {
		if _, err := os.Stat(filepath.Join(wizardDir, "config.yaml")); err == nil {
			configPath = filepath.Join(wizardDir, "config.yaml")
		} else if p := os.Getenv("UMAILSERVER_CONFIG"); p != "" {
			configPath = p
			if err := requireConfigFile(configPath); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// Check if this is first run (no config exists)
	if configPath == "" && config.CheckFirstRun(wizardDir) {
		fmt.Println()
		fmt.Println("Welcome to uMailServer!")
		fmt.Println("It looks like this is your first time running the server.")
		fmt.Println()

		// Run interactive setup
		wizard := config.NewSetupWizard()
		wizard.Config.Server.DataDir = wizardDir

		cfg, err := wizard.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
			os.Exit(1)
		}

		// Use the newly created config
		// The wizard lets the user change the data directory, so the config
		// lives wherever the wizard saved it, not necessarily under --data-dir.
		configPath = filepath.Join(cfg.Server.DataDir, "config.yaml")

		fmt.Println()
		fmt.Println("Setup complete! Starting server...")
		fmt.Println()
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override data directory if explicitly specified via flag
	if dataDir != "" {
		cfg.Server.DataDir = dataDir
	}

	// A reload signal must not kill the daemon (default SIGHUP action is to
	// terminate, skipping graceful shutdown). Live reload is not supported, so
	// say so and keep serving (F6216).
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			fmt.Fprintln(os.Stderr, "SIGHUP received: live config reload is not supported; use 'umailserver restart'")
		}
	}()

	// Create server
	srv, err := server.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Start server
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	if err := srv.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func cmdQuickstart(args []string) {
	// Define defaults to match install.sh
	defaultDataDir := "/var/lib/umailserver"
	defaultConfigDir := "/etc/umailserver"
	defaultConfigPath := defaultConfigDir + "/umailserver.yaml"

	// Command-line flags
	var dataDir string
	var configPath string

	fs := flag.NewFlagSet("quickstart", flag.ExitOnError)
	fs.StringVar(&dataDir, "data-dir", defaultDataDir, "Data directory")
	fs.StringVar(&configPath, "config", defaultConfigPath, "Config file path")
	// Get email from remaining args
	remaining := parseInterspersed(fs, args)
	if len(remaining) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver quickstart <email> [flags]")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
		os.Exit(1)
	}

	email := remaining[0]
	localPart, domain, ok := parseEmail(email)
	if !ok {
		fmt.Fprintf(os.Stderr, "Invalid email format: %s\n", email)
		os.Exit(1)
	}
	parts := []string{localPart, domain}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config file already exists: %s\n", configPath)
		fmt.Print("Overwrite? (y/N): ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
	}

	// Generate config
	fmt.Println("=== uMailServer Quickstart ===")
	fmt.Printf("Setting up for: %s\n\n", email)

	// Generate DKIM key
	dkimDir := filepath.Join(dataDir, "dkim")
	if err := os.MkdirAll(dkimDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create DKIM directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config directory: %v\n", err)
		os.Exit(1)
	}
	dkimKeyPath := filepath.Join(dkimDir, domain+".private.pem")

	fmt.Println("Generating DKIM key pair...")
	if err := generateDKIMKey(dkimKeyPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate DKIM key: %v\n", err)
		os.Exit(1)
	}

	// Read public key for DNS
	publicKeyPEM, err := os.ReadFile(filepath.Clean(dkimKeyPath + ".pub"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read DKIM public key: %v\n", err)
		os.Exit(1)
	}
	publicKey, err := dkimPublicKeyB64(publicKeyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid DKIM public key: %v\n", err)
		os.Exit(1)
	}

	// Write config
	config := fmt.Sprintf(`# uMailServer Configuration
# Generated by quickstart for %s

server:
  hostname: mail.%s
  data_dir: %s

tls:
  acme:
    enabled: true
    email: %s
    provider: letsencrypt

smtp:
  inbound:
    port: 25
    max_message_size: 52428800  # 50MB
    max_recipients: 100
  submission:
    port: 587
    require_auth: true
    require_tls: true
  submission_tls:
    port: 465

imap:
  port: 993

http:
  port: 443
  http_port: 80

admin:
  enabled: true
  port: 8443
  bind: 127.0.0.1

spam:
  reject_threshold: 9.0
  junk_threshold: 3.0
  greylisting:
    enabled: true
    delay: 5m

dkim:
  enabled: true
  selector: default
  domain: %s
  key_file: %s

domains:
  - name: %s
    max_accounts: 100
    max_mailbox_size: 5368709120  # 5GB
`, email, domain, dataDir, email, domain, dkimKeyPath, domain)

	if err := writeSecretFile(configPath, []byte(config), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Config written to: %s\n", configPath)
	fmt.Printf("✓ Data directory: %s\n", dataDir)

	// Initialize database
	fmt.Println("\nInitializing database...")
	dbPath := filepath.Join(dataDir, "umailserver.db")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create data directory: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Run pending migrations
	fmt.Println("Running database migrations...")
	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(database.BoltDB(), registry)
	if err := migrator.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run migrations: %v\n", err)
		os.Exit(1)
	}

	// Create domain
	if err := database.CreateDomain(&db.DomainData{
		Name:        domain,
		MaxAccounts: 100,
		IsActive:    true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create domain: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Domain created: %s\n", domain)

	// Create admin account with password prompt
	fmt.Print("\nEnter admin password: ")
	password := readNewPassword()
	if len(password) < 8 {
		fmt.Fprintf(os.Stderr, "Password must be at least 8 characters\n")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to hash password: %v\n", err)
		os.Exit(1)
	}

	if err := database.CreateAccount(&db.AccountData{
		Email:        email,
		LocalPart:    parts[0],
		Domain:       domain,
		PasswordHash: string(hash),
		IsAdmin:      true,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		if errors.Is(err, db.ErrAccountExists) {
			fmt.Printf("✓ Admin account already exists: %s\n", email)
		} else {
			fmt.Fprintf(os.Stderr, "Failed to create account: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("✓ Admin account created: %s\n", email)
	}

	// Print DNS records
	fmt.Println("\n=== Required DNS Records ===")
	fmt.Println("\n# MX Record:")
	fmt.Printf("%s.    IN    MX    10    mail.%s.\n\n", domain, domain)

	fmt.Println("# A Records:")
	fmt.Printf("mail.%s.    IN    A    <YOUR_SERVER_IP>\n\n", domain)

	fmt.Println("# SPF Record:")
	fmt.Printf("%s.    IN    TXT    \"v=spf1 mx ~all\"\n\n", domain)

	fmt.Println("# DKIM Record (default._domainkey):")
	fmt.Printf("default._domainkey.%s.    IN    TXT    %s\n\n", domain, dkimTXT(publicKey))

	fmt.Println("# DMARC Record:")
	fmt.Printf("_dmarc.%s.    IN    TXT    \"v=DMARC1; p=quarantine; rua=mailto:dmarc@%s\"\n\n", domain, domain)

	fmt.Println("=== Next Steps ===")
	fmt.Println("1. Update DNS records above with your actual server IP")
	fmt.Println("2. Start the server: sudo systemctl start umailserver")
	fmt.Println("   Or run directly: umailserver serve")
	fmt.Println("3. Access webmail at: https://mail.yourdomain.com")
	fmt.Println("4. Access admin panel at: https://127.0.0.1:8443")
}

func generateDKIMKey(keyPath string) error {
	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Write private key
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	if err := writeSecretFile(keyPath, pem.EncodeToMemory(privateKeyPEM), 0o600); err != nil {
		return err
	}

	// Write public key
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}

	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	return writeSecretFile(keyPath+".pub", pem.EncodeToMemory(publicKeyPEM), 0o644)
}

func readPassword() string {
	// #nosec G115 -- file descriptors are small positive integers on all supported platforms
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		// ReadPassword disables echo itself; MakeRaw would also disable signal
		// handling (Ctrl-C), and falling back to an echoing read on error
		// would leak the password to the screen (F6215).
		pw, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read password: %v\n", err)
			os.Exit(1)
		}
		return string(pw)
	}
	// Non-terminal input: consume one line without reading ahead into a later
	// prompt's input.
	var password strings.Builder
	var next [1]byte
	for {
		if n, _ := os.Stdin.Read(next[:]); n == 0 || next[0] == '\n' {
			break
		}
		password.WriteByte(next[0])
	}
	return strings.TrimSuffix(password.String(), "\r")
}

// readNewPassword prompts for a new password and, on a terminal, asks for it
// a second time and exits if the two entries differ (F6215).
func readNewPassword() string {
	pw := readPassword()
	// #nosec G115 -- file descriptors are small positive integers on all supported platforms
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("Confirm password: ")
		if again := readPassword(); again != pw {
			fmt.Fprintln(os.Stderr, "Passwords do not match")
			os.Exit(1)
		}
	}
	return pw
}

// getDataDir returns the data directory from config file, or default
func getDataDir() string {
	// Try to load config to get data_dir
	configPaths := configSearchPaths()
	var cfg *config.Config
	for _, p := range configPaths {
		// Check if config file actually exists before trying to load
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		var err error
		cfg, err = config.Load(p)
		if err == nil {
			break
		}
	}
	if cfg != nil && cfg.Server.DataDir != "" {
		return cfg.Server.DataDir
	}
	// Fallback to default
	return config.GetDefaultDataDir()
}

func cmdDomain(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver domain <subcommand>")
		fmt.Fprintln(os.Stderr, "Subcommands: add, list, dns, delete")
		os.Exit(1)
	}

	subcmd := args[0]

	// Load database using config's data_dir
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	switch subcmd {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver domain add <domain>")
			os.Exit(1)
		}
		domainName := args[1]

		// Generate DKIM key pair
		privKey, _, err := auth.GenerateDKIMKeyPair(2048)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate DKIM key: %v\n", err)
			os.Exit(1)
		}
		dkimPublicKey := auth.GetPublicKeyForDNS(privKey)
		dkimPrivateKeyPEM := string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privKey),
		}))

		if err := database.CreateDomain(&db.DomainData{
			Name:           domainName,
			MaxAccounts:    100,
			IsActive:       true,
			DKIMSelector:   "default",
			DKIMPublicKey:  dkimPublicKey,
			DKIMPrivateKey: dkimPrivateKeyPEM,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create domain: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Domain created: %s\n", domainName)
		fmt.Printf("✓ DKIM key generated (selector: default)\n")

	case "list":
		domains, err := database.ListDomains()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list domains: %v\n", err)
			os.Exit(1)
		}
		if len(domains) == 0 {
			fmt.Println("No domains found.")
			return
		}
		fmt.Println("Domains:")
		fmt.Println("--------")
		for _, d := range domains {
			status := "active"
			if !d.IsActive {
				status = "inactive"
			}
			n := 0
			if accts, err := database.ListAccountsByDomain(d.Name); err == nil {
				n = len(accts)
			}
			fmt.Printf("%-30s %s (%d/%d accounts)\n", d.Name, status, n, d.MaxAccounts)
		}

	case "dns":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver domain dns <domain>")
			os.Exit(1)
		}
		domainName := args[1]
		domain, err := database.GetDomain(domainName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Domain not found: %s\n", domainName)
			os.Exit(1)
		}

		fmt.Printf("\n=== DNS Records for %s ===\n\n", domain.Name)

		fmt.Println("# MX Record:")
		fmt.Printf("%s.    IN    MX    10    mail.%s.\n\n", domain.Name, domain.Name)

		fmt.Println("# A Record:")
		fmt.Printf("mail.%s.    IN    A    <YOUR_SERVER_IP>\n\n", domain.Name)

		fmt.Println("# SPF Record:")
		fmt.Printf("%s.    IN    TXT    \"v=spf1 mx ~all\"\n\n", domain.Name)

		fmt.Println("# DKIM Record (default._domainkey):")
		dkimKey := domain.DKIMPublicKey
		if dkimKey == "" {
			dkimKey = "<GENERATE_WITH: umailserver domain add>"
		}
		if domain.DKIMPublicKey == "" {
			fmt.Printf("default._domainkey.%s.    IN    TXT    \"v=DKIM1; k=rsa; p=%s\"\n\n", domain.Name, dkimKey)
		} else {
			fmt.Printf("default._domainkey.%s.    IN    TXT    %s\n\n", domain.Name, dkimTXT(dkimKey))
		}

		fmt.Println("# DMARC Record:")
		fmt.Printf("_dmarc.%s.    IN    TXT    \"v=DMARC1; p=quarantine; rua=mailto:dmarc@%s\"\n\n", domain.Name, domain.Name)

		fmt.Println("Replace <YOUR_SERVER_IP> with your actual server IP.")

	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver domain delete <domain>")
			os.Exit(1)
		}
		domainName := args[1]

		// Confirm deletion
		fmt.Printf("Are you sure you want to delete domain %s? This will delete all accounts! (y/N): ", domainName)
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}

		if err := database.DeleteDomain(domainName); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete domain: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Domain deleted: %s\n", domainName)

	default:
		fmt.Fprintf(os.Stderr, "Unknown domain subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdAccount(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver account <subcommand>")
		fmt.Fprintln(os.Stderr, "Subcommands: add, password, list, delete")
		os.Exit(1)
	}

	subcmd := args[0]

	// Load database using config's data_dir
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	switch subcmd {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver account add <email>")
			os.Exit(1)
		}
		addFS := flag.NewFlagSet("account add", flag.ExitOnError)
		passwordFlag := addFS.String("password", "", "Account password (visible in process list; prefer the prompt or UMAILSERVER_ACCOUNT_PASSWORD)")
		addPos := parseInterspersed(addFS, args[1:])
		if len(addPos) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver account add <email> [--password <pass>]")
			os.Exit(1)
		}
		email := addPos[0]
		localPart, domainPart, ok := parseEmail(email)
		if !ok {
			fmt.Fprintf(os.Stderr, "Invalid email format: %s\n", email)
			os.Exit(1)
		}
		parts := []string{localPart, domainPart}

		// Check if domain exists
		_, err := database.GetDomain(parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Domain not found: %s (create it first with 'umailserver domain add')\n", parts[1])
			os.Exit(1)
		}

		// Get password (from flag or prompt)
		// UMAILSERVER_ACCOUNT_PASSWORD keeps the secret out of argv (F6215).
		password := *passwordFlag
		if password == "" {
			password = os.Getenv("UMAILSERVER_ACCOUNT_PASSWORD")
		}
		if password == "" {
			fmt.Print("Enter password: ")
			password = readNewPassword()
		}
		if len(password) < 8 {
			fmt.Fprintf(os.Stderr, "Password must be at least 8 characters\n")
			os.Exit(1)
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to hash password: %v\n", err)
			os.Exit(1)
		}

		if err := database.CreateAccount(&db.AccountData{
			Email:        email,
			LocalPart:    parts[0],
			Domain:       parts[1],
			PasswordHash: string(hash),
			IsAdmin:      false,
			IsActive:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}); err != nil {
			if errors.Is(err, db.ErrAccountExists) {
				fmt.Fprintf(os.Stderr, "Account already exists: %s\n", email)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Failed to create account: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Account created: %s\n", email)

	case "list":
		var domain string
		if len(args) >= 2 {
			domain = args[1]
		}

		var accounts []*db.AccountData
		var err error

		if domain != "" {
			accounts, err = database.ListAccountsByDomain(domain)
		} else {
			// List all accounts
			domains, err := database.ListDomains()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to list domains: %v\n", err)
				os.Exit(1)
			}
			for _, d := range domains {
				domainAccounts, _ := database.ListAccountsByDomain(d.Name)
				accounts = append(accounts, domainAccounts...)
			}
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list accounts: %v\n", err)
			os.Exit(1)
		}

		if len(accounts) == 0 {
			fmt.Println("No accounts found.")
			return
		}

		fmt.Println("Accounts:")
		fmt.Println("---------")
		for _, a := range accounts {
			status := "active"
			if !a.IsActive {
				status = "inactive"
			}
			admin := ""
			if a.IsAdmin {
				admin = "[admin]"
			}
			fmt.Printf("%-40s %s %s\n", a.Email, status, admin)
		}

	case "password":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver account password <email>")
			os.Exit(1)
		}
		email := args[1]
		localPart, domainPart, ok := parseEmail(email)
		if !ok {
			fmt.Fprintf(os.Stderr, "Invalid email format: %s\n", email)
			os.Exit(1)
		}
		parts := []string{localPart, domainPart}

		account, err := database.GetAccount(parts[1], parts[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Account not found: %s\n", email)
			os.Exit(1)
		}

		fmt.Print("Enter new password: ")
		password := readNewPassword()
		if len(password) < 8 {
			fmt.Fprintf(os.Stderr, "Password must be at least 8 characters\n")
			os.Exit(1)
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to hash password: %v\n", err)
			os.Exit(1)
		}

		account.PasswordHash = string(hash)
		account.UpdatedAt = time.Now()

		if err := database.UpdateAccount(account); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update password: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Password updated for: %s\n", email)

	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver account delete <email>")
			os.Exit(1)
		}
		email := args[1]
		localPart, domainPart, ok := parseEmail(email)
		if !ok {
			fmt.Fprintf(os.Stderr, "Invalid email format: %s\n", email)
			os.Exit(1)
		}
		parts := []string{localPart, domainPart}

		// Confirm deletion
		fmt.Printf("Are you sure you want to delete account %s? (y/N): ", email)
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}

		if err := database.DeleteAccount(parts[1], parts[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete account: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Account deleted: %s\n", email)

	default:
		fmt.Fprintf(os.Stderr, "Unknown account subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdQueue(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver queue <subcommand>")
		fmt.Fprintln(os.Stderr, "Subcommands: list, retry, flush, drop")
		os.Exit(1)
	}

	subcmd := args[0]

	// Open database using config's data_dir
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	switch subcmd {
	case "list":
		entries, err := allQueueEntries(database)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list queue: %v\n", err)
			os.Exit(1)
		}
		if len(entries) == 0 {
			fmt.Println("Queue is empty.")
			return
		}
		fmt.Printf("%-20s %-30s %-30s %-10s %s\n", "ID", "From", "To", "Status", "Retries")
		fmt.Println(strings.Repeat("-", 110))
		for _, e := range entries {
			fmt.Printf("%-20s %-30s %-30s %-10s %d\n", e.ID, e.From, strings.Join(e.To, ","), e.Status, e.RetryCount)
		}
	case "retry":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver queue retry <id>")
			os.Exit(1)
		}
		entry, err := database.GetQueueEntry(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Queue entry not found: %s\n", args[1])
			os.Exit(1)
		}
		entry.Status = "pending"
		entry.NextRetry = time.Now()
		entry.RetryCount = 0
		entry.LastError = ""
		if err := database.UpdateQueueEntry(entry); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to retry entry: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Queue entry retried: %s\n", args[1])
	case "flush":
		entries, err := allQueueEntries(database)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to flush queue: %v\n", err)
			os.Exit(1)
		}
		count := 0
		for _, e := range entries {
			if e.Status == "failed" {
				e.Status = "pending"
				e.NextRetry = time.Now()
				e.RetryCount = 0
				e.LastError = ""
				if err := database.UpdateQueueEntry(e); err == nil {
					count++
				}
			}
		}
		fmt.Printf("Flushed %d failed entries\n", count)
	case "drop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver queue drop <id>")
			os.Exit(1)
		}
		if err := database.Dequeue(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to drop entry: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Queue entry dropped: %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown queue subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdCheck(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver check <type>")
		fmt.Fprintln(os.Stderr, "Types: dns, tls, deliverability")
		os.Exit(1)
	}

	checkType := args[0]

	// Load config
	dataDir := "./data"
	var cfg *config.Config
	for _, p := range configSearchPaths() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if c, err := config.Load(p); err == nil {
			cfg = c
			break
		}
	}
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "warning: no usable config found; using hostname \"localhost\" (set UMAILSERVER_CONFIG or run from the config directory)")
		cfg = &config.Config{
			Server: config.ServerConfig{
				Hostname: "localhost",
				DataDir:  dataDir,
			},
		}
	}

	diagnostics := cli.NewDiagnostics(cfg)

	switch checkType {
	case "dns":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver check dns <domain>")
			os.Exit(1)
		}
		results, err := diagnostics.CheckDNS(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "DNS check failed: %v\n", err)
			os.Exit(1)
		}
		cli.PrintDNSResults(results)
		// A failed check must be visible to scripts (F6217).
		for _, r := range results {
			if r.Status == "fail" {
				os.Exit(1)
			}
		}

	case "tls":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver check tls <hostname>")
			os.Exit(1)
		}
		result, err := diagnostics.CheckTLS(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "TLS check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("TLS Check: %s\n", result.Message)
		if result.Valid {
			fmt.Printf("  Protocol: %s\n", result.Protocol)
			fmt.Printf("  Version:  %s\n", result.Version)
			fmt.Printf("  Cipher:   %s\n", result.Cipher)
		} else {
			os.Exit(1)
		}

	case "deliverability":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver check deliverability <domain>")
			os.Exit(1)
		}
		result, err := diagnostics.CheckDeliverability(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deliverability check failed: %v\n", err)
			os.Exit(1)
		}
		cli.PrintDeliverabilityResults(result)
		if result.OverallScore == "fail" {
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown check type: %s\n", checkType)
		os.Exit(1)
	}
}

func cmdTest(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver test <type>")
		fmt.Fprintln(os.Stderr, "Types: send")
		os.Exit(1)
	}

	testType := args[0]

	switch testType {
	case "send":
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: umailserver test send <from> <to> <subject>")
			os.Exit(1)
		}
		// The command never sent anything yet reported success (exit 0).
		fmt.Fprintln(os.Stderr, "test send is not implemented: no message was sent")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown test type: %s\n", testType)
		os.Exit(1)
	}
}

func cmdBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	configPath := fs.String("config", "./umailserver.yaml", "Path to config file")
	password := fs.String("password", "", "Backup encryption password (empty = store unencrypted)")
	pos := parseInterspersed(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver backup <backup-directory> [--config <path>] [--password <pass>]")
		os.Exit(1)
	}

	// BackupManager treats the path as a DIRECTORY: a timestamped
	// umailserver_backup_<TS>.tar.gz[.enc] file is created inside it.
	backupPath := pos[0]

	if err := requireConfigFile(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	bm := cli.NewBackupManager(cfg)
	// UMAILSERVER_BACKUP_PASSWORD keeps the secret out of argv, which any
	// local user can read via /proc/<pid>/cmdline or ps (F5916).
	if *password == "" {
		*password = os.Getenv("UMAILSERVER_BACKUP_PASSWORD")
	}
	if *password != "" {
		bm.SetPassword(*password)
	}
	if err := bm.Backup(backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	configPath := fs.String("config", "./umailserver.yaml", "Path to config file")
	password := fs.String("password", "", "Backup encryption password (required for encrypted backups)")
	pos := parseInterspersed(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver restore <backup-file> [--config <path>] [--password <pass>]")
		os.Exit(1)
	}

	backupFile := pos[0]

	if err := requireConfigFile(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	bm := cli.NewBackupManager(cfg)
	// UMAILSERVER_BACKUP_PASSWORD keeps the secret out of argv, which any
	// local user can read via /proc/<pid>/cmdline or ps (F5916).
	if *password == "" {
		*password = os.Getenv("UMAILSERVER_BACKUP_PASSWORD")
	}
	if *password != "" {
		bm.SetPassword(*password)
	}
	if err := bm.Restore(backupFile); err != nil {
		fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	sourceType := fs.String("type", "", "Source type (imap, dovecot, mbox)")
	source := fs.String("source", "", "Source path or URL")
	username := fs.String("username", "", "Source username (for IMAP)")
	password := fs.String("password", "", "Source password (for IMAP)")
	targetUser := fs.String("target", "", "Target user email")
	dryRun := fs.Bool("dry-run", false, "Dry run mode")
	passwdFile := fs.String("passwd-file", "", "Password file (for Dovecot)")
	configFlag := fs.String("config", "./umailserver.yaml", "Path to config file")

	_ = fs.Parse(args)

	if *sourceType == "" || *source == "" {
		fmt.Fprintln(os.Stderr, "Usage: umailserver migrate --type <type> --source <source>")
		fmt.Fprintln(os.Stderr, "Types: imap, dovecot, mbox")
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  umailserver migrate --type imap --source imaps://oldserver.com --username user@old.com --target user@new.com")
		fmt.Fprintln(os.Stderr, "  umailserver migrate --type dovecot --source /var/mail --passwd-file /etc/dovecot/users")
		fmt.Fprintln(os.Stderr, "  umailserver migrate --type mbox --source /path/to/mail/*.mbox")
		os.Exit(1)
	}

	// Load config and database
	if err := requireConfigFile(*configFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	dataDir := cfg.Server.DataDir
	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(cfg.Server.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open message store: %v\n", err)
		os.Exit(1)
	}

	mm := cli.NewMigrationManager(database, msgStore, nil)

	opts := cli.MigrateOptions{
		SourceType: *sourceType,
		SourceURL:  *source,
		SourcePath: *source,
		Username:   *username,
		Password:   *password,
		TargetUser: *targetUser,
		DryRun:     *dryRun,
	}

	switch *sourceType {
	case "imap":
		if err := mm.MigrateFromIMAP(opts); err != nil {
			fmt.Fprintf(os.Stderr, "IMAP migration failed: %v\n", err)
			os.Exit(1)
		}
	case "dovecot":
		if err := mm.MigrateFromDovecot(*source, *passwdFile); err != nil {
			fmt.Fprintf(os.Stderr, "Dovecot migration failed: %v\n", err)
			os.Exit(1)
		}
	case "mbox":
		if err := mm.MigrateFromMBOX(*source); err != nil {
			fmt.Fprintf(os.Stderr, "MBOX migration failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown source type: %s\n", *sourceType)
		os.Exit(1)
	}
}

func cmdDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver db <subcommand>")
		fmt.Fprintln(os.Stderr, "Subcommands: status, migrate, rollback")
		os.Exit(1)
	}

	subcmd := args[0]

	switch subcmd {
	case "status":
		cmdDBStatus(args[1:])
	case "migrate":
		cmdDBMigrate(args[1:])
	case "rollback":
		cmdDBRollback(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown db subcommand: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: umailserver db <subcommand>")
		fmt.Fprintln(os.Stderr, "Subcommands: status, migrate, rollback")
		os.Exit(1)
	}
}

func cmdDBStatus(args []string) {
	dataDir := config.GetDefaultDataDir()
	if len(args) > 0 {
		dataDir = args[0]
	}

	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(database.BoltDB(), registry)

	status, err := migrator.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get migration status: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Database migrations:\n")
	fmt.Printf("  Applied: %d\n", status.Applied)
	fmt.Printf("  Pending: %d\n", status.Pending)
	fmt.Printf("  Total:   %d\n", status.Total)

	if status.Pending > 0 {
		fmt.Println("\nRun 'umailserver db migrate' to apply pending migrations.")
	}
}

func cmdDBMigrate(args []string) {
	dataDir := config.GetDefaultDataDir()
	if len(args) > 0 {
		dataDir = args[0]
	}

	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	fmt.Println("Running database migrations...")

	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(database.BoltDB(), registry)

	if err := migrator.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	status, _ := migrator.Status()
	fmt.Printf("Migration complete. Applied: %d, Pending: %d\n", status.Applied, status.Pending)
}

func cmdDBRollback(args []string) {
	dataDir := config.GetDefaultDataDir()
	if len(args) > 0 {
		dataDir = args[0]
	}

	dbPath := filepath.Join(dataDir, "umailserver.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	fmt.Println("Rolling back last migration...")

	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(database.BoltDB(), registry)

	if err := migrator.Rollback(); err != nil {
		fmt.Fprintf(os.Stderr, "Rollback failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Rollback complete.")
}

// lifecycleDataDir resolves the data directory for status/stop/restart: an
// explicit argument wins, otherwise the configured data_dir (the directory the
// running server writes its PID file to), otherwise the default (F6214).
func lifecycleDataDir(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return getDataDir()
}

// readLivePID reads the PID file and reports whether the recorded process
// exists. A PID file naming a dead process is stale and is removed by the
// caller's choice via stale=true (F6214).
func readLivePID(pidFile *server.PIDFile) (pid int, running bool, stale bool) {
	pid, err := pidFile.Read()
	if err != nil || pid <= 0 {
		return 0, false, false
	}
	if !processAlive(pid) {
		return pid, false, true
	}
	return pid, true, false
}

func cmdStatus(args []string) {
	dataDir := lifecycleDataDir(args)

	pidFile := server.NewPIDFile(dataDir)
	pid, running, stale := readLivePID(pidFile)
	if !running {
		if stale {
			fmt.Printf("Status: not running (stale PID file for PID %d)\n", pid)
		} else {
			fmt.Println("Status: not running")
		}
		// Conventional status exit code: 3 = program is not running.
		os.Exit(3)
	}

	fmt.Printf("Status: running\n")
	fmt.Printf("PID: %d\n", pid)
}

func cmdStop(args []string) {
	dataDir := lifecycleDataDir(args)

	pidFile := server.NewPIDFile(dataDir)
	pid, running, stale := readLivePID(pidFile)
	if !running {
		if stale {
			_ = pidFile.Remove()
			fmt.Printf("Server is not running (removed stale PID file for PID %d)\n", pid)
		} else {
			fmt.Println("Server is not running")
		}
		os.Exit(0)
	}

	fmt.Printf("Stopping server (PID: %d)...\n", pid)

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find process: %v\n", err)
		os.Exit(1)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to signal process: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Stop signal sent")
}

func cmdRestart(args []string) {
	dataDir := lifecycleDataDir(args)

	// Stop if running
	pidFile := server.NewPIDFile(dataDir)
	if pid, running, stale := readLivePID(pidFile); running {
		fmt.Printf("Stopping server (PID: %d)...\n", pid)
		proc, err := os.FindProcess(pid)
		if err != nil || proc.Signal(os.Interrupt) != nil {
			fmt.Fprintf(os.Stderr, "Failed to signal process %d\n", pid)
			os.Exit(1)
		}
		// Wait for the old process to exit; starting while it still holds the
		// ports and PID file would fail or race (F6214).
		deadline := time.Now().Add(60 * time.Second)
		for processAlive(pid) {
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr, "Server (PID %d) did not stop within 60s\n", pid)
				os.Exit(1)
			}
			time.Sleep(200 * time.Millisecond)
		}
	} else if stale {
		_ = pidFile.Remove()
	}

	// Start again
	fmt.Println("Starting server...")
	cmdServe([]string{"--data-dir", dataDir})
}

// allQueueEntries returns every queue entry regardless of status. GetPendingQueue
// only returns "pending" entries due within a window, so list never showed
// failed entries and flush (which retries failed ones) could never find any.
func allQueueEntries(database *db.DB) ([]*db.QueueEntry, error) {
	var entries []*db.QueueEntry
	err := database.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var e db.QueueEntry
		if err := json.Unmarshal(value, &e); err != nil {
			return err
		}
		entries = append(entries, &e)
		return nil
	})
	return entries, err
}

// parseInterspersed parses fs flags wherever they appear in args and returns
// the positional arguments in order. flag.FlagSet.Parse stops at the first
// positional argument, so "backup <dir> --password p" (the documented usage)
// silently ignored --config and --password and wrote an unencrypted backup
// from the default config (F5620). Arguments after "--" stay positional.
func parseInterspersed(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		before := len(args)
		_ = fs.Parse(args)
		rest := fs.Args()
		if len(rest) == 0 {
			return pos
		}
		// Parse consumed a "--" terminator: everything after it is positional.
		consumed := before - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...)
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}
