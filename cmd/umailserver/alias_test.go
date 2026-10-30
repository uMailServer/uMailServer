package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func aliasTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.CreateDomain(&db.DomainData{Name: "example.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&db.AccountData{Email: "bob@example.test", LocalPart: "bob", Domain: "example.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	return d
}

func runAliasCmd(d *db.DB, input string, args ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code = runAlias(d, args, strings.NewReader(input), &o, &e)
	return code, o.String(), e.String()
}

func TestAliasAddListDelete(t *testing.T) {
	d := aliasTestDB(t)

	if code, out, e := runAliasCmd(d, "", "add", "Postmaster@Example.test", "bob@example.test"); code != 0 {
		t.Fatalf("add: %d %s %s", code, out, e)
	}
	if a, err := d.GetAlias("example.test", "postmaster"); err != nil || a.Target != "bob@example.test" || !a.IsActive {
		t.Fatalf("stored alias = %+v (err %v)", a, err)
	}
	if code, _, e := runAliasCmd(d, "", "add", "postmaster@example.test", "bob@example.test"); code == 0 || !strings.Contains(e, "already exists") {
		t.Fatalf("duplicate add: %d %q", code, e)
	}
	if _, out, _ := runAliasCmd(d, "", "list"); !strings.Contains(out, "postmaster@example.test -> bob@example.test") {
		t.Fatalf("list = %q", out)
	}
	if _, out, _ := runAliasCmd(d, "", "list", "other.test"); !strings.Contains(out, "No aliases found") {
		t.Fatalf("filtered list = %q", out)
	}
	// declining the prompt keeps the alias; --yes removes it
	if code, out, _ := runAliasCmd(d, "n\n", "delete", "postmaster@example.test"); code != 0 || !strings.Contains(out, "Aborted") {
		t.Fatalf("declined delete: %d %q", code, out)
	}
	if _, err := d.GetAlias("example.test", "postmaster"); err != nil {
		t.Fatal("alias removed although the prompt was declined")
	}
	if code, _, e := runAliasCmd(d, "", "delete", "postmaster@example.test", "--yes"); code != 0 {
		t.Fatalf("delete: %d %s", code, e)
	}
	if _, err := d.GetAlias("example.test", "postmaster"); err == nil {
		t.Fatal("alias still present after delete")
	}
}

func TestAliasAddRejectsBadInput(t *testing.T) {
	d := aliasTestDB(t)
	for _, c := range []struct{ alias, target, want string }{
		{"nodomain", "bob@example.test", "Invalid alias"},
		{"a@example.test", "bad", "Invalid target"},
		{"a@unhosted.test", "bob@example.test", "Domain not found"},
		{"a@example.test", "ghost@example.test", "Target account not found"},
	} {
		if code, _, e := runAliasCmd(d, "", "add", c.alias, c.target); code == 0 || !strings.Contains(e, c.want) {
			t.Errorf("add %s %s: code %d, stderr %q, want %q", c.alias, c.target, code, e, c.want)
		}
	}
	if aliases, _ := d.ListAliases(); len(aliases) != 0 {
		t.Errorf("%d alias(es) stored despite rejections", len(aliases))
	}
}
