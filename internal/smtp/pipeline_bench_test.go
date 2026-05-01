package smtp

import (
	"net"
	"testing"
)

// realisticMessage returns a benign-looking message with the headers a normal
// inbound mail would carry, so the heuristic rules don't spuriously fire.
func realisticMessage() *MessageContext {
	body := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Quarterly report\r\n" +
		"Date: Mon, 1 Jan 2026 12:00:00 +0000\r\n" +
		"Message-Id: <abc123@example.com>\r\n" +
		"\r\n" +
		"Hello, please find attached the report.\r\n")
	ctx := NewMessageContext(net.ParseIP("203.0.113.5"), "sender@example.com",
		[]string{"recipient@example.com"}, body)
	ctx.Headers["Subject"] = []string{"Quarterly report"}
	ctx.Headers["Date"] = []string{"Mon, 1 Jan 2026 12:00:00 +0000"}
	ctx.Headers["Message-Id"] = []string{"<abc123@example.com>"}
	ctx.Headers["From"] = []string{"sender@example.com"}
	ctx.Headers["To"] = []string{"recipient@example.com"}
	return ctx
}

// BenchmarkPipeline_Minimal measures the cheapest realistic configuration:
// the dispatch loop in Pipeline.Process plus two CPU-only stages
// (Heuristic + Score). This is the floor any inbound message pays.
func BenchmarkPipeline_Minimal(b *testing.B) {
	pipeline := NewPipeline(nil)
	pipeline.AddStage(NewHeuristicStage())
	pipeline.AddStage(NewScoreStage(10.0, 5.0))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx := realisticMessage()
		_, _ = pipeline.Process(ctx)
	}
}

// BenchmarkPipeline_MinimalNoCtxAlloc isolates Pipeline.Process from
// MessageContext allocation, so we can see what dispatch+stages actually cost
// once the context already exists. The stage state is reset between iterations
// so heuristic match accumulation doesn't drift.
func BenchmarkPipeline_MinimalNoCtxAlloc(b *testing.B) {
	pipeline := NewPipeline(nil)
	pipeline.AddStage(NewHeuristicStage())
	pipeline.AddStage(NewScoreStage(10.0, 5.0))

	ctx := realisticMessage()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx.SpamScore = 0
		ctx.SpamResult = SpamResult{}
		ctx.Rejected = false
		_, _ = pipeline.Process(ctx)
	}
}

// BenchmarkHeuristicStage_Clean measures the heuristic stage in isolation on
// a message that triggers no rules — best-case path through the rule list.
func BenchmarkHeuristicStage_Clean(b *testing.B) {
	stage := NewHeuristicStage()
	ctx := realisticMessage()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx.SpamScore = 0
		ctx.SpamResult = SpamResult{}
		stage.Process(ctx)
	}
}

// BenchmarkHeuristicStage_AllRulesMatch is the worst-case shape: every default
// rule fires, so the Reasons slice is appended four times per call.
func BenchmarkHeuristicStage_AllRulesMatch(b *testing.B) {
	stage := NewHeuristicStage()
	ip := net.ParseIP("203.0.113.5")
	ctx := NewMessageContext(ip, "sender@example.com",
		[]string{"recipient@example.com"}, []byte("body"))
	// No Subject, no Date, no Message-Id → 3 of 4 rules match.
	// Add ALL_CAPS_SUBJECT to make it 4.
	ctx.Headers["Subject"] = []string{"THIS IS AN URGENT NOTICE"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx.SpamScore = 0
		ctx.SpamResult = SpamResult{}
		stage.Process(ctx)
	}
}

// BenchmarkScoreStage benches the final scoring stage in isolation. Should be
// trivial — included as a sanity floor.
func BenchmarkScoreStage(b *testing.B) {
	stage := NewScoreStage(10.0, 5.0)
	ctx := realisticMessage()
	ctx.SpamScore = 3.5

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stage.Process(ctx)
	}
}

// BenchmarkNewMessageContext measures the cost of allocating a fresh
// MessageContext per inbound message — done once per DATA command in
// production.
func BenchmarkNewMessageContext(b *testing.B) {
	ip := net.ParseIP("203.0.113.5")
	body := []byte("From: a@b.c\r\nTo: d@e.f\r\nSubject: x\r\n\r\nbody\r\n")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewMessageContext(ip, "a@b.c", []string{"d@e.f"}, body)
	}
}
