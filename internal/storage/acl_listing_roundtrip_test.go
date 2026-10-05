package storage

import (
	"fmt"
	"reflect"
	"testing"
)

func TestACLListingPlainMailbox(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetACL("owner", "Projects", "reader", ACLRead, "owner"); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListMailboxesSharedWith("reader")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"owner:Projects"}) {
		t.Fatalf("CONTROL invalid: %v", got)
	}
	fmt.Println("CONTROL EXPECTED: [owner:Projects] ACTUAL:", got)
}
func TestACLListingColonMailbox(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetACL("owner", "Projects:2026", "reader", ACLRead, "owner"); err != nil {
		t.Fatal(err)
	}
	rights, err := db.GetACL("owner", "Projects:2026", "reader")
	if err != nil || rights != ACLRead {
		t.Fatalf("fixture invalid rights=%v err=%v", rights, err)
	}
	got, err := db.ListMailboxesSharedWith("reader")
	if err != nil {
		t.Fatal(err)
	}
	owned, err := db.ListGranteesMailboxes("owner")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("EXPECTED: shared=[owner:Projects:2026] owned=[Projects:2026] ACTUAL: shared=%v owned=%v\n", got, owned)
	if !reflect.DeepEqual(got, []string{"owner:Projects:2026"}) || !reflect.DeepEqual(owned, []string{"Projects:2026"}) {
		t.Fatal("DEFECT F4776 mailbox names truncated by list parsing")
	}
}
func TestACLListingColonMailboxEdges(t *testing.T) {
	for _, mailbox := range []string{"Projects:2026:Q1", ":Projects", "Projects:"} {
		t.Run(mailbox, func(t *testing.T) {
			db := openTestDB(t)
			for _, g := range []string{"reader", "other"} {
				if err := db.SetACL("owner", mailbox, g, ACLRead, "owner"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := db.ListMailboxesSharedWith("reader")
			if err != nil {
				t.Fatal(err)
			}
			owned, err := db.ListGranteesMailboxes("owner")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"owner:" + mailbox}) || !reflect.DeepEqual(owned, []string{mailbox}) {
				t.Fatalf("edge round trip shared=%v owned=%v", got, owned)
			}
		})
	}
}
