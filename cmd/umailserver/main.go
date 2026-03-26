package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/server"
)

var (
	Version   = "dev"
	BuildDate = "unknown"
	GitCommit = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
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
	case "version":
		fmt.Printf("uMailServer %s (%s) built %s\n", Version, GitCommit, BuildDate)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`uMailServer - One binary. Complete email.

Usage: umailserver <command> [flags]

Commands:
  serve        Start the mail server
  quickstart   Generate config and create first account
  domain       Domain management (add, list, dns)
  account      Account management (add, password, list, delete)
  queue        Queue management (list, retry, flush, drop)
  check        Diagnostics (dns, tls, deliverability)
  test         Test utilities (send)
  backup       Create backup
  restore      Restore from backup
  migrate      Import from other mail servers
  version      Show version

Examples:
  umailserver quickstart you@example.com
  umailserver serve --config /etc/umailserver.yaml
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
	fs.Parse(args)

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override data directory if specified
	if dataDir != "" {
		cfg.Server.DataDir = dataDir
	}

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
	if len(args) < 1 {
		fmt.Println("Usage: umailserver quickstart <email>")
		os.Exit(1)
	}

	email := args[0]
	fmt.Printf("Quickstart for %s\n", email)
	fmt.Println("(not yet implemented - will generate config and create account)")
}

func cmdDomain(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver domain <subcommand>")
		fmt.Println("Subcommands: add, list, dns, delete")
		os.Exit(1)
	}

	subcmd := args[0]
	fmt.Printf("Domain command: %s\n", subcmd)

	switch subcmd {
	case "add":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver domain add <domain>")
			os.Exit(1)
		}
		fmt.Printf("Adding domain: %s\n", args[1])
	case "list":
		fmt.Println("Listing domains...")
	case "dns":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver domain dns <domain>")
			os.Exit(1)
		}
		fmt.Printf("DNS records for: %s\n", args[1])
	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver domain delete <domain>")
			os.Exit(1)
		}
		fmt.Printf("Deleting domain: %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown domain subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdAccount(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver account <subcommand>")
		fmt.Println("Subcommands: add, password, list, delete")
		os.Exit(1)
	}

	subcmd := args[0]
	fmt.Printf("Account command: %s\n", subcmd)

	switch subcmd {
	case "add":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver account add <email> [flags]")
			os.Exit(1)
		}
		fmt.Printf("Adding account: %s\n", args[1])
	case "list":
		fmt.Println("Listing accounts...")
	case "password":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver account password <email>")
			os.Exit(1)
		}
		fmt.Printf("Changing password for: %s\n", args[1])
	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver account delete <email>")
			os.Exit(1)
		}
		fmt.Printf("Deleting account: %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown account subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdQueue(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver queue <subcommand>")
		fmt.Println("Subcommands: list, retry, flush, drop")
		os.Exit(1)
	}

	subcmd := args[0]
	fmt.Printf("Queue command: %s\n", subcmd)

	switch subcmd {
	case "list":
		fmt.Println("Listing queue entries...")
	case "retry":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver queue retry <id>")
			os.Exit(1)
		}
		fmt.Printf("Retrying queue entry: %s\n", args[1])
	case "flush":
		fmt.Println("Flushing queue...")
	case "drop":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver queue drop <id>")
			os.Exit(1)
		}
		fmt.Printf("Dropping queue entry: %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown queue subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func cmdCheck(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver check <type>")
		fmt.Println("Types: dns, tls, deliverability")
		os.Exit(1)
	}

	checkType := args[0]
	fmt.Printf("Check command: %s\n", checkType)

	switch checkType {
	case "dns":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver check dns <domain>")
			os.Exit(1)
		}
		fmt.Printf("Checking DNS for: %s\n", args[1])
	case "tls":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver check tls <domain>")
			os.Exit(1)
		}
		fmt.Printf("Checking TLS for: %s\n", args[1])
	case "deliverability":
		if len(args) < 2 {
			fmt.Println("Usage: umailserver check deliverability <domain>")
			os.Exit(1)
		}
		fmt.Printf("Checking deliverability for: %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown check type: %s\n", checkType)
		os.Exit(1)
	}
}

func cmdTest(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver test <type>")
		fmt.Println("Types: send")
		os.Exit(1)
	}

	testType := args[0]
	fmt.Printf("Test command: %s\n", testType)

	switch testType {
	case "send":
		if len(args) < 4 {
			fmt.Println("Usage: umailserver test send <from> <to> <subject>")
			os.Exit(1)
		}
		fmt.Printf("Sending test email from %s to %s\n", args[1], args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown test type: %s\n", testType)
		os.Exit(1)
	}
}

func cmdBackup(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver backup <path>")
		os.Exit(1)
	}
	fmt.Printf("Backup to: %s\n", args[0])
}

func cmdRestore(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver restore <path>")
		os.Exit(1)
	}
	fmt.Printf("Restore from: %s\n", args[0])
}

func cmdMigrate(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: umailserver migrate --source <type>")
		fmt.Println("Source types: postfix, dovecot, maildir")
		os.Exit(1)
	}
	fmt.Println("Migrate command (not yet implemented)")
}
