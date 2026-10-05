package circuitbreaker

import "testing"

func TestExecutePanicReleasesHalfOpenAdmission(t *testing.T) {
	cb := New(DefaultConfig())
	cb.state = StateHalfOpen
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan interface{}, 1)
	go func() {
		defer func() { done <- recover() }()
		_ = cb.Execute(func() error {
			close(started)
			<-release
			panic("callback panic")
		})
	}()
	<-started
	if got := cb.Metrics().HalfOpenRequests; got != 1 {
		close(release)
		<-done
		t.Fatalf("in-flight admissions = %d, want 1", got)
	}
	close(release)
	if got := <-done; got != "callback panic" {
		t.Fatalf("panic = %v, want callback panic", got)
	}
	if got := cb.Metrics().HalfOpenRequests; got != 0 {
		t.Fatalf("completed admissions = %d, want 0", got)
	}
	if !cb.Allow() {
		t.Fatal("recovered panic prevented the next probe")
	}
}
