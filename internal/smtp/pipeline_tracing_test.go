package smtp

import (
	"context"
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/tracing"
)

type recordingStage struct {
	name   string
	result PipelineResult
	called bool
}

func (s *recordingStage) Name() string { return s.name }
func (s *recordingStage) Process(_ *MessageContext) PipelineResult {
	s.called = true
	return s.result
}

func newTestPipeline(t *testing.T, stages ...PipelineStage) *Pipeline {
	t.Helper()
	p := NewPipeline(nil)
	for _, st := range stages {
		p.AddStage(st)
	}
	return p
}

func newMsgCtx() *MessageContext {
	return NewMessageContext(net.IPv4(127, 0, 0, 1), "from@x", []string{"to@x"}, []byte("hi"))
}

func TestPipelineProcess_NoTracingProvider_RunsAllStages(t *testing.T) {
	a := &recordingStage{name: "a", result: ResultAccept}
	b := &recordingStage{name: "b", result: ResultAccept}
	p := newTestPipeline(t, a, b)

	res, err := p.Process(newMsgCtx())
	if err != nil || res != ResultAccept {
		t.Fatalf("Process: res=%v err=%v", res, err)
	}
	if !a.called || !b.called {
		t.Error("both stages should run")
	}
}

func TestPipelineProcess_WithDisabledProvider_StillRunsStages(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	a := &recordingStage{name: "stage_a", result: ResultAccept}
	p := newTestPipeline(t, a)
	p.SetTracingProvider(provider)

	res, _ := p.Process(newMsgCtx())
	if res != ResultAccept {
		t.Fatalf("expected accept, got %v", res)
	}
	if !a.called {
		t.Error("stage should run even with tracing disabled")
	}
}

// enrichingStage populates stage-result fields on the message context the
// way real auth/spam stages do, so we can verify the tracing attribute
// enrichment path runs without panicking.
type enrichingStage struct{ name string }

func (s *enrichingStage) Name() string { return s.name }
func (s *enrichingStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SPFResult = SPFResult{Result: "pass", Domain: "x.example"}
	ctx.DKIMResult = DKIMResult{Valid: true, Domain: "x.example"}
	ctx.DMARCResult = DMARCResult{Result: "pass", Policy: "reject"}
	ctx.ARCResult = ARCResult{Result: "pass"}
	ctx.SpamScore = 1.5
	return ResultAccept
}

func TestPipelineProcess_TracingEnriches_FromMessageContext(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	p := newTestPipeline(t, &enrichingStage{name: "enrich"})
	p.SetTracingProvider(provider)

	res, err := p.Process(newMsgCtx())
	if err != nil || res != ResultAccept {
		t.Fatalf("Process: res=%v err=%v", res, err)
	}
}

func TestPipelineProcess_TracingEnabled_StagesRunAndStopOnReject(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	a := &recordingStage{name: "a", result: ResultAccept}
	b := &recordingStage{name: "b", result: ResultReject}
	c := &recordingStage{name: "c", result: ResultAccept}
	p := newTestPipeline(t, a, b, c)
	p.SetTracingProvider(provider)

	msg := newMsgCtx()
	msg.RejectionMessage = "blocked"

	res, err := p.Process(msg)
	if res != ResultReject || err == nil {
		t.Fatalf("want reject + error, got res=%v err=%v", res, err)
	}
	if !a.called || !b.called {
		t.Error("a and b should run")
	}
	if c.called {
		t.Error("c should not run after reject")
	}
}
