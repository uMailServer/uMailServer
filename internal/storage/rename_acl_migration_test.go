package storage

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestRenameMailboxMigratesEveryACLGrant is the regression test for F5095:
// RenameMailbox used to Put/Delete ACL keys while iterating the shared "acl"
// bucket with a live cursor. Mutations invalidated the cursor, so on a server
// with other shared mailboxes some grants were skipped: they stayed on the old
// name (and applied to any new mailbox later created with it) and were missing
// on the renamed one.
func TestRenameMailboxMigratesEveryACLGrant(t *testing.T) {
	cases := []struct{ oldName, newName string }{
		{"Team", "Work"},       // target sorts after source
		{"Team", "Archive"},    // target sorts before source
		{"Team:Q1", "Work:Q2"}, // colons in mailbox names
	}
	for _, tc := range cases {
		t.Run(tc.oldName+"->"+tc.newName, func(t *testing.T) {
			db, err := OpenDatabase(filepath.Join(t.TempDir(), "mail.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			for i := 0; i < 400; i++ {
				owner := fmt.Sprintf("user%03d@example.com", i)
				for _, g := range []string{"alice@example.com", "bob@example.com"} {
					if err := db.SetACL(owner, "Projects", g, ACLLookup|ACLRead, owner); err != nil {
						t.Fatal(err)
					}
				}
			}
			owner := "user050@example.com"
			if err := db.CreateMailbox(owner, tc.oldName); err != nil {
				t.Fatal(err)
			}
			const n = 12
			for i := 0; i < n; i++ {
				if err := db.SetACL(owner, tc.oldName, fmt.Sprintf("member%02d@example.com", i), ACLLookup|ACLRead, owner); err != nil {
					t.Fatal(err)
				}
			}

			if err := db.RenameMailbox(owner, tc.oldName, tc.newName); err != nil {
				t.Fatal(err)
			}

			if old, _ := db.ListACL(owner, tc.oldName); len(old) != 0 {
				t.Fatalf("%d grants left on old name %q", len(old), tc.oldName)
			}
			for i := 0; i < n; i++ {
				g := fmt.Sprintf("member%02d@example.com", i)
				if r, _ := db.GetACL(owner, tc.newName, g); r != ACLLookup|ACLRead {
					t.Fatalf("%s rights on %q = %v, want lr", g, tc.newName, r)
				}
			}
			if r, _ := db.GetACL("user000@example.com", "Projects", "alice@example.com"); r == 0 {
				t.Fatal("unrelated owner's grant was lost")
			}
		})
	}
}
