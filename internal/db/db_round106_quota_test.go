package db

import "testing"

func TestIncrementQuotaReleaseClampsAtZero(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAccount(&AccountData{LocalPart: "u", Domain: "q.test", QuotaLimit: 100}); err != nil {
		t.Fatal(err)
	}
	if err := d.IncrementQuota("q.test", "u", 10); err != nil {
		t.Fatal(err)
	}
	if err := d.IncrementQuota("q.test", "u", -50); err != nil {
		t.Fatal(err)
	}
	a, _ := d.GetAccount("q.test", "u")
	if a.QuotaUsed != 0 {
		t.Fatalf("QuotaUsed=%d want 0", a.QuotaUsed)
	}
	// Headroom must not exceed the limit afterwards.
	if err := d.IncrementQuota("q.test", "u", 101); err == nil {
		t.Fatal("expected quota exceeded")
	}
}

func TestReconcileQuota(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAccount(&AccountData{LocalPart: "u", Domain: "q.test"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileQuota("q.test", "u", 42); err != nil {
		t.Fatal(err)
	}
	if a, _ := d.GetAccount("q.test", "u"); a.QuotaUsed != 42 {
		t.Fatalf("got %d", a.QuotaUsed)
	}
	_ = d.ReconcileQuota("q.test", "u", -5)
	if a, _ := d.GetAccount("q.test", "u"); a.QuotaUsed != 0 {
		t.Fatalf("got %d", a.QuotaUsed)
	}
	if err := d.ReconcileQuota("q.test", "nope", 1); err == nil {
		t.Fatal("want error")
	}
}
