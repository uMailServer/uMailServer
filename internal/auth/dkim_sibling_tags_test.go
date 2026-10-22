package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

// Round-27 sibling fixes for the round-26 DKIM audit, both in the
// parser-leniency class of F6922/F6843/parseStrictUint:
//
//   - parseCopiedHeaders split z= pairs on "=" but RFC 6376 §3.5 defines
//     z= as "hdr-name:hdr-value" pairs separated by "|", so every entry
//     without an "=" in its value was silently dropped.
//   - parseDKIMPublicKey accepted key records without v=, but RFC 6376
//     §3.6.1 requires the tag and mandates DKIM1 as its value.

func TestParseCopiedHeadersSplitsOnColon(t *testing.T) {
	got := parseCopiedHeaders("From:alice@example.com|To:bob@example.net")
	if got["from"] != "alice@example.com" {
		t.Errorf("FAIL: z= From entry dropped (want alice@example.com, got %q) — RFC 6376 §3.5 z= pairs are hdr-name:hdr-value", got["from"])
	}
	if got["to"] != "bob@example.net" {
		t.Errorf("FAIL: z= To entry dropped (want bob@example.net, got %q)", got["to"])
	}
	// Control: a value containing "=" survives a first-colon split verbatim.
	withEq := parseCopiedHeaders("Subject:a=b")
	if withEq["subject"] != "a=b" {
		t.Errorf("z= value altered: want %q, got %q", "a=b", withEq["subject"])
	}
}

func TestParseDKIMPublicKeyRequiresVersionTag(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	dnsKey := GetPublicKeyForDNS(privateKey)

	// Control: a conforming record parses.
	if _, _, err := parseDKIMPublicKey("v=DKIM1; k=rsa; p=" + dnsKey); err != nil {
		t.Fatalf("control conforming record rejected: %v", err)
	}

	// Missing v= must be rejected (RFC 6376 §3.6.1: the tag is required,
	// with DKIM1 as its only defined value).
	if _, _, err := parseDKIMPublicKey("k=rsa; p=" + dnsKey); err == nil {
		t.Errorf("FAIL: key record without v= accepted (RFC 6376 §3.6.1: v= is required, value DKIM1)")
	}

	// k= absent keeps its documented rsa default (§3.6.1).
	if key, keyType, err := parseDKIMPublicKey("v=DKIM1; p=" + dnsKey); err != nil || keyType != "rsa" || key == nil {
		t.Errorf("k= absent should default to rsa per §3.6.1, got type %q err %v", keyType, err)
	}
}
