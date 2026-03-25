package main

import (
	"fmt"
	"os"
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

// Placeholder command implementations
func cmdServe(args []string) {
	fmt.Println("Starting uMailServer...")
	fmt.Println("(not yet implemented - will start SMTP, IMAP, and HTTP servers)")
}

func cmdQuickstart(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver quickstart <email>")
		os.Exit(1)
	}
	fmt.Printf("Quickstart for %s\n", args[0])
	fmt.Println("(not yet implemented)")
}

func cmdDomain(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver domain <subcommand>")
		fmt.Println("Subcommands: add, list, dns, delete")
		os.Exit(1)
	}
	fmt.Printf("Domain command: %s\n", args[0])
}

func cmdAccount(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver account <subcommand>")
		fmt.Println("Subcommands: add, password, list, delete")
		os.Exit(1)
	}
	fmt.Printf("Account command: %s\n", args[0])
}

func cmdQueue(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver queue <subcommand>")
		fmt.Println("Subcommands: list, retry, flush, drop")
		os.Exit(1)
	}
	fmt.Printf("Queue command: %s\n", args[0])
}

func cmdCheck(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver check <type>")
		fmt.Println("Types: dns, tls, deliverability")
		os.Exit(1)
	}
	fmt.Printf("Check command: %s\n", args[0])
}

func cmdTest(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: umailserver test <type>")
		fmt.Println("Types: send")
		os.Exit(1)
	}
	fmt.Printf("Test command: %s\n", args[0])
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
	if len(args) < 1 {
		fmt.Println("Usage: umailserver migrate --source <type>")
		os.Exit(1)
	}
	fmt.Println("Migrate command")
}
