package config

// NOTE: SetupWizard is wired into cmdServe first-run flow (cmd/umailserver/main.go:117).
// The interactive wizard guides new admins through initial server setup.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetupWizard handles interactive first-time setup
type SetupWizard struct {
	reader *bufio.Reader
	Config *Config // Public access
}

// NewSetupWizard creates a new setup wizard
func NewSetupWizard() *SetupWizard {
	return &SetupWizard{
		reader: bufio.NewReader(os.Stdin),
		Config: DefaultConfig(),
	}
}

// Run starts the interactive setup process
func (w *SetupWizard) Run() (*Config, error) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          uMailServer - Interactive Setup Wizard              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Get data directory
	dataDir, err := w.askString("Data directory", w.Config.Server.DataDir)
	if err != nil {
		return nil, err
	}
	w.Config.Server.DataDir = dataDir

	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Get hostname
	hostname, err := w.askString("Server hostname (FQDN)", w.Config.Server.Hostname)
	if err != nil {
		return nil, err
	}
	w.Config.Server.Hostname = hostname

	// Ask which services to enable
	fmt.Println()
	fmt.Println("┌─ Service Configuration ─")

	w.Config.SMTP.Inbound.Enabled = w.askBool("Enable SMTP (inbound/MX)?", true)
	if w.Config.SMTP.Inbound.Enabled {
		w.Config.SMTP.Inbound.Port = w.askInt("SMTP inbound port", 25)
	}

	w.Config.SMTP.Submission.Enabled = w.askBool("Enable SMTP submission (STARTTLS)?", true)
	if w.Config.SMTP.Submission.Enabled {
		w.Config.SMTP.Submission.Port = w.askInt("SMTP submission port", 587)
	}

	w.Config.IMAP.Enabled = w.askBool("Enable IMAP?", true)
	if w.Config.IMAP.Enabled {
		w.Config.IMAP.Port = w.askInt("IMAP port", 993)
	}

	w.Config.POP3.Enabled = w.askBool("Enable POP3?", false)
	if w.Config.POP3.Enabled {
		w.Config.POP3.Port = w.askInt("POP3 port", 995)
	}

	w.Config.Admin.Enabled = w.askBool("Enable admin panel?", true)
	if w.Config.Admin.Enabled {
		w.Config.Admin.Port = w.askInt("Admin panel port", 8443)
	}

	// TLS configuration
	fmt.Println()
	fmt.Println("┌─ TLS Configuration ─")
	useACME := w.askBool("Use Let's Encrypt (ACME) for automatic certificates?", true)
	if useACME {
		w.Config.TLS.ACME.Enabled = true
		w.Config.TLS.ACME.Email, _ = w.askString("ACME email address", "")
		w.Config.TLS.ACME.Provider = "letsencrypt"
	} else {
		fmt.Println("You'll need to manually configure TLS certificates.")
	}

	// Spam filtering
	fmt.Println()
	fmt.Println("┌─ Spam Filtering ─")
	w.Config.Spam.Enabled = w.askBool("Enable spam filtering?", true)
	if w.Config.Spam.Enabled {
		w.Config.Spam.Bayesian.Enabled = w.askBool("Enable Bayesian classifier?", true)
		w.Config.Spam.Greylisting.Enabled = w.askBool("Enable greylisting?", true)
	}

	// Logging
	fmt.Println()
	fmt.Println("┌─ Logging Configuration ─")
	w.Config.Logging.Level, _ = w.askChoice("Log level", []string{"debug", "info", "warn", "error"}, "info")
	w.Config.Logging.Format, _ = w.askChoice("Log format", []string{"json", "text"}, "json")

	// Save configuration
	fmt.Println()
	fmt.Println("┌─ Saving Configuration ─")

	configPath := filepath.Join(dataDir, "config.yaml")
	if err := w.Save(configPath); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✓ Configuration saved to: %s\n", configPath)
	fmt.Println()

	// Create required directories
	if err := w.Config.EnsureDataDir(); err != nil {
		return nil, err
	}
	fmt.Println("✓ Created data directories")

	return w.Config, nil
}

// Save writes the configuration to file
func (w *SetupWizard) Save(path string) error {
	data, err := yaml.Marshal(w.Config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// askString asks for a string input with default value
func (w *SetupWizard) askString(prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}

	input, err := w.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}

	return input, nil
}

// askBool asks for a yes/no answer
func (w *SetupWizard) askBool(prompt string, defaultVal bool) bool {
	defaultStr := "Y/n"
	if !defaultVal {
		defaultStr = "y/N"
	}

	fmt.Printf("%s [%s]: ", prompt, defaultStr)

	input, err := w.reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}

	input = strings.ToLower(strings.TrimSpace(input))

	if input == "" {
		return defaultVal
	}

	return input == "y" || input == "yes"
}

// askInt asks for an integer
func (w *SetupWizard) askInt(prompt string, defaultVal int) int {
	fmt.Printf("%s [%d]: ", prompt, defaultVal)

	input, err := w.reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}

	val, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Invalid number, using default: %d\n", defaultVal)
		return defaultVal
	}

	return val
}

// askChoice asks the user to select from a list of options
func (w *SetupWizard) askChoice(prompt string, options []string, defaultVal string) (string, error) {
	fmt.Printf("%s:\n", prompt)
	for i, opt := range options {
		marker := "  "
		if opt == defaultVal {
			marker = "* "
		}
		fmt.Printf("  %s%d) %s\n", marker, i+1, opt)
	}

	defaultIdx := 0
	for i, opt := range options {
		if opt == defaultVal {
			defaultIdx = i + 1
			break
		}
	}

	fmt.Printf("Selection [%d]: ", defaultIdx)

	input, err := w.reader.ReadString('\n')
	if err != nil {
		return defaultVal, nil
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}

	idx, err := strconv.Atoi(input)
	if err != nil || idx < 1 || idx > len(options) {
		fmt.Printf("Invalid selection, using default: %s\n", defaultVal)
		return defaultVal, nil
	}

	return options[idx-1], nil
}

// CheckFirstRun checks if this is the first run (no config exists)
func CheckFirstRun(dataDir string) bool {
	configPath := filepath.Join(dataDir, "config.yaml")
	_, err := os.Stat(configPath)
	return os.IsNotExist(err)
}

// GetDefaultDataDir returns the default data directory
func GetDefaultDataDir() string {
	// Try XDG_DATA_HOME first
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData != "" {
		return filepath.Join(xdgData, "umailserver")
	}

	// Fall back to home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return ".umailserver"
	}

	// Use ~/.local/share/umailserver on Unix, ~/umailserver on Windows
	return filepath.Join(home, ".local", "share", "umailserver")
}
