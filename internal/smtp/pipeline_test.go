package smtp

import (
	"net"
	"testing"
)

func TestMessageContext(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	data := []byte("Subject: Test\r\n\r\nBody")

	ctx := NewMessageContext(ip, from, to, data)

	if ctx.RemoteIP.String() != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1, got %s", ctx.RemoteIP)
	}
	if ctx.From != from {
		t.Errorf("Expected from %s, got %s", from, ctx.From)
	}
	if len(ctx.To) != 1 || ctx.To[0] != to[0] {
		t.Errorf("Expected to %v, got %v", to, ctx.To)
	}
}

func TestPipeline(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)

	// Add a test stage
	pipeline.AddStage(&testStage{name: "TestStage"})

	t.Run("AcceptMessage", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result, err := pipeline.Process(ctx)
		if err != nil {
			t.Fatalf("Pipeline failed: %v", err)
		}
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
	})
}

func TestRateLimitStage(t *testing.T) {
	stage := NewRateLimitStage()

	t.Run("UnderLimit", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
	})

	t.Run("OverLimit", func(t *testing.T) {
		stage := NewRateLimitStage() // Fresh stage
		ip := net.ParseIP("192.168.1.2")

		// Send 31 messages (over limit)
		for i := 0; i < 31; i++ {
			ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
			stage.Process(ctx)
		}

		// 31st message should be rejected
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject after limit, got %d", result)
		}
	})
}

func TestGreylistStage(t *testing.T) {
	stage := NewGreylistStage()

	t.Run("FirstAttempt", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject on first attempt, got %d", result)
		}
		if ctx.RejectionCode != 451 {
			t.Errorf("Expected code 451, got %d", ctx.RejectionCode)
		}
	})

	t.Run("SecondAttemptTooSoon", func(t *testing.T) {
		stage := NewGreylistStage() // Fresh stage
		ip := net.ParseIP("192.168.1.3")

		// First attempt
		ctx1 := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		stage.Process(ctx1)

		// Second attempt immediately
		ctx2 := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		result := stage.Process(ctx2)
		if result != ResultReject {
			t.Errorf("Expected ResultReject on second attempt (too soon), got %d", result)
		}
	})
}

func TestHeuristicStage(t *testing.T) {
	stage := NewHeuristicStage()

	t.Run("EmptySubject", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		// No subject header

		stage.Process(ctx)
		if ctx.SpamScore < 1.0 {
			t.Errorf("Expected spam score >= 1.0 for empty subject, got %f", ctx.SpamScore)
		}
	})

	t.Run("AllCapsSubject", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.Headers["Subject"] = []string{"THIS IS SPAM"}

		stage.Process(ctx)
		if ctx.SpamScore < 2.0 {
			t.Errorf("Expected spam score >= 2.0 for all caps subject, got %f", ctx.SpamScore)
		}
	})

	t.Run("MissingDate", func(t *testing.T) {
		stage := NewHeuristicStage() // Fresh stage
		ip := net.ParseIP("192.168.1.4")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		// No Date header

		stage.Process(ctx)
		if ctx.SpamScore < 1.0 {
			t.Errorf("Expected spam score >= 1.0 for missing date, got %f", ctx.SpamScore)
		}
	})
}

func TestScoreStage(t *testing.T) {
	t.Run("Inbox", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 1.0

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept for inbox, got %d", result)
		}
		if ctx.SpamResult.Verdict != "inbox" {
			t.Errorf("Expected verdict inbox, got %s", ctx.SpamResult.Verdict)
		}
	})

	t.Run("Junk", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 5.0

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept for junk (delivered to junk folder), got %d", result)
		}
		if ctx.SpamResult.Verdict != "junk" {
			t.Errorf("Expected verdict junk, got %s", ctx.SpamResult.Verdict)
		}
	})

	t.Run("Reject", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 10.0

		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject for high score, got %d", result)
		}
		if ctx.SpamResult.Verdict != "reject" {
			t.Errorf("Expected verdict reject, got %s", ctx.SpamResult.Verdict)
		}
	})
}

func TestReverseIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "1.1.168.192"},
		{"10.0.0.1", "1.0.0.10"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := reverseIP(tt.input)
		if got != tt.expected {
			t.Errorf("reverseIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// Test helpers

type testStage struct {
	name string
}

func (s *testStage) Name() string { return s.name }

func (s *testStage) Process(ctx *MessageContext) PipelineResult {
	return ResultAccept
}

type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...interface{}) {}
func (l *testLogger) Info(msg string, args ...interface{})  {}
func (l *testLogger) Warn(msg string, args ...interface{})  {}
func (l *testLogger) Error(msg string, args ...interface{}) {}
