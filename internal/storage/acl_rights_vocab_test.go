package storage

// Regression tests for the ACL rights vocabulary defect: ParseACLRights
// rejected the RFC 4314-required characters 't' (delete-messages: \Deleted)
// and 'e' (expunge), mapped 'p'->\Deleted (RFC: p=post), 'k'->expunge (RFC:
// k=create-mailboxes), 'x'->create-mailboxes (RFC: x=delete-mailbox), and
// swapped w/i relative to the RFC (w=write=flags, i=insert=APPEND/COPY).
// ACLRights.String() emitted the same wrong letters, and handleListRights
// advertised rights the parser rejected. These tests pin the RFC 4314 §2.2.1
// vocabulary that the model represents: l r s w i t e k.

import "testing"

func TestParseACLRightsRFC4314Vocabulary(t *testing.T) {
	cases := []struct {
		char  string
		right ACLRights
	}{
		{"l", ACLLookup},
		{"r", ACLRead},
		{"s", ACLSeen},
		{"w", ACLWriteSeen}, // flags other than \Seen/\Deleted
		{"i", ACLWrite},     // insert: APPEND, COPY into
		{"t", ACLDelete},    // set/clear \Deleted
		{"e", ACLExpunge},   // EXPUNGE
		{"k", ACLCreate},    // create-mailboxes
	}
	for _, tc := range cases {
		rights, negative, err := ParseACLRights(tc.char)
		if err != nil {
			t.Fatalf("RFC 4314 right %q rejected: %v", tc.char, err)
		}
		if negative {
			t.Fatalf("right %q unexpectedly marked negative", tc.char)
		}
		if rights != tc.right {
			t.Fatalf("right %q parsed to %v, want %v", tc.char, rights, tc.right)
		}
	}

	// Combined string including the previously rejected t and e.
	rights, _, err := ParseACLRights("te")
	if err != nil {
		t.Fatalf("\"te\" rejected: %v", err)
	}
	if rights != ACLDelete|ACLExpunge {
		t.Fatalf("\"te\" parsed to %v, want Delete|Expunge", rights)
	}

	// Negative flag still parses (the revocation path applies it with &^).
	rights, negative, err := ParseACLRights("-e")
	if err != nil {
		t.Fatalf("\"-e\" rejected: %v", err)
	}
	if !negative || rights != ACLExpunge {
		t.Fatalf("\"-e\" = %v, negative=%v; want Expunge, true", rights, negative)
	}

	// Letters for rights this model does not represent stay rejected rather
	// than silently mis-mapped.
	for _, bad := range []string{"p", "x", "c", "d", "a"} {
		if _, _, err := ParseACLRights(bad); err == nil {
			t.Fatalf("unmodeled right %q unexpectedly accepted", bad)
		}
	}
}

func TestACLRightsStringRFC4314Vocabulary(t *testing.T) {
	full := ACLLookup | ACLRead | ACLSeen | ACLWriteSeen | ACLWrite |
		ACLDelete | ACLExpunge | ACLCreate
	if got := full.String(); got != "lrswitek" {
		t.Fatalf("full rights String() = %q, want %q", got, "lrswitek")
	}

	// Round-trip: every letter String() emits must parse back to the same
	// bit, so GETACL/MYRIGHTS output is SETACL-compatible.
	for _, ch := range full.String() {
		rights, _, err := ParseACLRights(string(ch))
		if err != nil {
			t.Fatalf("String() emitted %q which ParseACLRights rejects: %v", ch, err)
		}
		if got := ACLRights(0) | rights; got != 0 && full&got == 0 {
			t.Fatalf("String() emitted %q which does not correspond to a bit in %v", ch, full)
		}
	}

	// Empty rights render as an empty string (no phantom letters).
	if got := ACLRights(0).String(); got != "" {
		t.Fatalf("empty rights String() = %q, want \"\"", got)
	}
}
