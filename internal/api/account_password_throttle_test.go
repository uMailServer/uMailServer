package api

// Regression tests promoted from the audit proofs in .temp_files (F5030).

import (
	"fmt"
	"net/http"
	"testing"
)

func a5030Change(s *Server, tok, cur string) int {
	return a5028Do(s, http.MethodPost, "/api/v1/account/password", tok,
		fmt.Sprintf(`{"current_password":%q,"new_password":"N3wPassw0rd!"}`, cur))
}

// Control: the right current password changes the password.
func TestAccountPasswordThrottle_Control(t *testing.T) {
	s, _ := a5028Setup(t)
	if c := a5030Change(s, a5028Session(t), "Passw0rd!x"); c != http.StatusOK {
		t.Fatalf("INVALID control: status=%d", c)
	}
}

// Defect: the self-service password endpoint (not behind the API rate
// limiter) is an unthrottled oracle for the current password: a stolen
// session can guess it without limit, bypassing the login lockout, and the
// right guess is accepted after any number of misses.
func TestAccountPasswordThrottle_WrongGuessesThrottled(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	var codes []int
	for i := 0; i < 20; i++ {
		codes = append(codes, a5030Change(s, tok, fmt.Sprintf("guess-%d", i)))
	}
	hit := a5030Change(s, tok, "Passw0rd!x")
	throttled := 0
	for _, c := range codes {
		if c == http.StatusTooManyRequests {
			throttled++
		}
	}
	t.Logf("EXPECTED: wrong guesses throttled (429) after the login budget (5) | ACTUAL: 429s=%d of 20, final correct guess=%d", throttled, hit)
	if throttled == 0 {
		t.Fatalf("regression F5030: 20 wrong current_password guesses never throttled; correct guess then status=%d", hit)
	}
}

// Edge: four misses then the right password succeeds and resets the budget.
func TestAccountPasswordThrottle_SuccessResets(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	for i := 0; i < 4; i++ {
		if c := a5030Change(s, tok, fmt.Sprintf("g%d", i)); c != http.StatusForbidden {
			t.Fatalf("miss %d=%d want 403", i, c)
		}
	}
	if c := a5030Change(s, tok, "Passw0rd!x"); c != http.StatusOK {
		t.Fatalf("correct=%d", c)
	}
	s.accountLoginMu.Lock()
	_, left := s.accountLoginAttempts[accountLoginKey("127.0.0.1", "bob@ex.com")]
	s.accountLoginMu.Unlock()
	if left {
		t.Fatalf("budget not cleared after success")
	}
}

// Edge: the budget is shared with login (same client IP, F6134) and keyed case-insensitively; once
// exhausted, even the correct password is refused.
func TestAccountPasswordThrottle_SharedBudget(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	for i := 0; i < 5; i++ {
		a5030Change(s, tok, fmt.Sprintf("g%d", i))
	}
	if c := a5030Change(s, tok, "Passw0rd!x"); c != http.StatusTooManyRequests {
		t.Fatalf("correct after budget=%d want 429", c)
	}
	if c := a5026Login(s, "127.0.0.1", "BOB@ex.com", "Passw0rd!x"); c != http.StatusTooManyRequests {
		t.Fatalf("login after budget=%d want 429", c)
	}
}
