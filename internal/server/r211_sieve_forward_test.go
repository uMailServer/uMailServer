package server

import "testing"

// F6881: an account forward is a per-message action, but a Sieve script with
// several fileinto targets ran the whole local delivery (including the
// forward) once per target, so the forwarding address got one copy per folder.
func TestR211SieveMultiFolderForwardsOnce(t *testing.T) {
	srv := junkRTServer(t, true)
	acc, err := srv.database.GetAccount("test.com", "alice")
	if err != nil {
		t.Fatal(err)
	}
	acc.ForwardTo, acc.ForwardKeepCopy = "far@ext.example", true
	if err := srv.database.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	sieveDTScript(t, srv, "alice@test.com", "require \"fileinto\";\nfileinto \"A\";\nfileinto \"B\";\n")
	sieveDTSend(t, srv, "s@ext.example", []string{"alice@test.com"}, junkRTClean)
	if a, b := sieveDTCount(t, srv, "alice@test.com", "A"), sieveDTCount(t, srv, "alice@test.com", "B"); a != 1 || b != 1 {
		t.Fatalf("folders A=%d B=%d, want 1/1", a, b)
	}
	if n := len(sieveDTQueued(t, srv, "far@ext.example")); n != 1 {
		t.Fatalf("forwarding address got %d copies, want 1", n)
	}
}
