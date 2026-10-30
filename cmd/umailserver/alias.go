package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/umailserver/umailserver/internal/db"
)

// runAlias implements `umailserver alias add|list|delete` against an open
// database and returns the process exit code. The checks mirror the admin API
// (internal/api/server_aliases.go): the alias domain must be hosted and the
// target must be an existing account.
func runAlias(database *db.DB, args []string, in io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "Usage: umailserver alias <add|list|delete>")
		return 1
	}
	switch args[0] {
	case "add":
		rest := positionalArgs(args[1:])
		if len(rest) != 2 {
			fmt.Fprintln(stderr, "Usage: umailserver alias add <alias@domain> <target@domain>")
			return 1
		}
		alias, target := strings.ToLower(rest[0]), strings.ToLower(rest[1])
		aliasUser, aliasDomain, ok := parseEmail(alias)
		if !ok {
			fmt.Fprintf(stderr, "Invalid alias address: %s\n", rest[0])
			return 1
		}
		targetUser, targetDomain, ok := parseEmail(target)
		if !ok {
			fmt.Fprintf(stderr, "Invalid target address: %s\n", rest[1])
			return 1
		}
		if _, err := database.GetDomain(aliasDomain); err != nil {
			fmt.Fprintf(stderr, "Domain not found: %s\n", aliasDomain)
			return 1
		}
		if acc, err := database.GetAccount(targetDomain, targetUser); err != nil || acc == nil {
			fmt.Fprintf(stderr, "Target account not found: %s\n", target)
			return 1
		}
		err := database.CreateAlias(&db.AliasData{Alias: aliasUser, Domain: aliasDomain, Target: target, IsActive: true})
		if errors.Is(err, db.ErrAliasExists) {
			fmt.Fprintf(stderr, "Alias already exists: %s\n", alias)
			return 1
		}
		if err != nil {
			fmt.Fprintf(stderr, "Failed to create alias: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ Alias created: %s -> %s\n", alias, target)
	case "list":
		aliases, err := database.ListAliases()
		if err != nil {
			fmt.Fprintf(stderr, "Failed to list aliases: %v\n", err)
			return 1
		}
		var only string
		if rest := positionalArgs(args[1:]); len(rest) > 0 {
			only = strings.ToLower(rest[0])
		}
		sort.Slice(aliases, func(i, j int) bool {
			if aliases[i].Domain != aliases[j].Domain {
				return aliases[i].Domain < aliases[j].Domain
			}
			return aliases[i].Alias < aliases[j].Alias
		})
		n := 0
		for _, a := range aliases {
			if only != "" && a.Domain != only {
				continue
			}
			state := ""
			if !a.IsActive {
				state = "  (disabled)"
			}
			fmt.Fprintf(stdout, "%s@%s -> %s%s\n", a.Alias, a.Domain, a.Target, state)
			n++
		}
		if n == 0 {
			fmt.Fprintln(stdout, "No aliases found.")
		}
	case "delete":
		assumeYes := hasYesFlag(args)
		rest := positionalArgs(args[1:])
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "Usage: umailserver alias delete <alias@domain> [--yes]")
			return 1
		}
		alias := strings.ToLower(rest[0])
		aliasUser, aliasDomain, ok := parseEmail(alias)
		if !ok {
			fmt.Fprintf(stderr, "Invalid alias address: %s\n", rest[0])
			return 1
		}
		if _, err := database.GetAlias(aliasDomain, aliasUser); err != nil {
			fmt.Fprintf(stderr, "Alias not found: %s\n", alias)
			return 1
		}
		if !confirmDestructive(fmt.Sprintf("Delete alias %s?", alias), assumeYes, in, stdout) {
			fmt.Fprintln(stdout, "Aborted.")
			return 0
		}
		if err := database.DeleteAlias(aliasDomain, aliasUser); err != nil {
			fmt.Fprintf(stderr, "Failed to delete alias: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ Alias deleted: %s\n", alias)
	default:
		fmt.Fprintf(stderr, "Unknown alias subcommand: %s\n", args[0])
		return 1
	}
	return 0
}

func cmdAlias(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: umailserver alias <add|list|delete>")
		os.Exit(1)
	}
	database, err := db.Open(getDatabasePath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	code := runAlias(database, args, os.Stdin, os.Stdout, os.Stderr)
	database.Close()
	os.Exit(code)
}
