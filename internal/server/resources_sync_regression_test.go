package server

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestResourceMonitorSynchronizationControl(t *testing.T) {
	m := NewResourceMonitor(ResourceLimits{MaxConnections: 1}, nil)
	calls := 0
	m.SetConnectionLimitCallback(func() { calls++; m.GetStats() })
	if !m.AddConnection() || m.AddConnection() || calls != 1 {
		t.Fatal("connection control failed")
	}
}

func TestResourceMonitorSynchronizationConcurrent(t *testing.T) {
	for _, name := range []string{"memory_callback", "goroutine_callback", "connection_callback", "memory_limit"} {
		t.Run(name, func(t *testing.T) {
			m := NewResourceMonitor(ResourceLimits{MaxMemoryMB: 1, MaxGoroutines: 1, MaxConnections: 1}, nil)
			data := make([]byte, 5*1024*1024)
			callback := func() {}
			var write, read func()
			switch name {
			case "memory_callback":
				m.SetMemoryLimitCallback(callback)
				write = func() { m.SetMemoryLimitCallback(callback) }
				read = m.checkResources
			case "goroutine_callback":
				m.SetGoroutineLimitCallback(callback)
				write = func() { m.SetGoroutineLimitCallback(callback) }
				read = m.checkResources
			case "connection_callback":
				m.AddConnection()
				m.SetConnectionLimitCallback(callback)
				write = func() { m.SetConnectionLimitCallback(callback) }
				read = func() { m.AddConnection() }
			case "memory_limit":
				write = func() { m.SetMaxMemory(1) }
				read = m.checkResources
			}
			start := make(chan struct{})
			stop := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for {
					select {
					case <-stop:
						return
					default:
						write()
						runtime.Gosched()
					}
				}
			}()
			close(start)
			for i := 0; i < 100; i++ {
				read()
			}
			close(stop)
			wg.Wait()
			runtime.KeepAlive(data)
		})
	}
}

func TestResourceMonitorSynchronizationEdges(t *testing.T) {
	for _, name := range []string{"memory", "goroutine", "connection"} {
		t.Run(name, func(t *testing.T) {
			m := NewResourceMonitor(ResourceLimits{MaxMemoryMB: 1, MaxGoroutines: 1, MaxConnections: 1}, nil)
			data := make([]byte, 5*1024*1024)
			calls := 0
			var set func(func())
			var check func()
			switch name {
			case "memory":
				set = m.SetMemoryLimitCallback
				check = m.checkResources
			case "goroutine":
				set = m.SetGoroutineLimitCallback
				check = m.checkResources
			case "connection":
				set = m.SetConnectionLimitCallback
				m.AddConnection()
				check = func() { m.AddConnection() }
			}
			set(func() { calls++; set(nil); m.GetStats(); m.SetMaxMemory(1) })
			done := make(chan struct{})
			go func() { check(); check(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("callback reentry deadlocked")
			}
			runtime.KeepAlive(data)
			if calls != 1 {
				t.Fatalf("callback removal: got %d calls, want 1", calls)
			}
			set(nil)
			check()
		})
	}
}

type resourceMonitorGateLogger struct {
	noopLogger
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *resourceMonitorGateLogger) Warn(msg string, args ...interface{}) {
	l.once.Do(func() { close(l.entered); <-l.release })
}

func TestResourceMonitorSynchronizationSnapshot(t *testing.T) {
	for _, name := range []string{"memory", "goroutine", "connection"} {
		for _, remove := range []bool{false, true} {
			suffix := "replace"
			if remove {
				suffix = "remove"
			}
			t.Run(name+"/"+suffix, func(t *testing.T) {
				logger := &resourceMonitorGateLogger{entered: make(chan struct{}), release: make(chan struct{})}
				release := sync.OnceFunc(func() { close(logger.release) })
				t.Cleanup(release)
				limits := ResourceLimits{}
				switch name {
				case "memory":
					limits.MaxMemoryMB = 1
				case "goroutine":
					limits.MaxGoroutines = 1
				case "connection":
					limits.MaxConnections = 1
				}
				m := NewResourceMonitor(limits, logger)
				data := make([]byte, 5*1024*1024)
				oldCalls, newCalls := 0, 0
				var set func(func())
				var check func()
				switch name {
				case "memory":
					set = m.SetMemoryLimitCallback
					check = m.checkResources
				case "goroutine":
					set = m.SetGoroutineLimitCallback
					check = m.checkResources
				case "connection":
					set = m.SetConnectionLimitCallback
					m.AddConnection()
					check = func() { m.AddConnection() }
				}
				set(func() { oldCalls++ })
				done := make(chan struct{})
				go func() { check(); close(done) }()
				select {
				case <-logger.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("resource warning did not enter gate")
				}
				if remove {
					set(nil)
				} else {
					set(func() { newCalls++ })
				}
				release()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("resource check deadlocked")
				}
				if oldCalls != 1 || newCalls != 0 {
					t.Fatalf("callback snapshot: old=%d new=%d; want old=1 new=0", oldCalls, newCalls)
				}
				check()
				runtime.KeepAlive(data)
				wantNew := 1
				if remove {
					wantNew = 0
				}
				if oldCalls != 1 || newCalls != wantNew {
					t.Fatalf("next check: old=%d new=%d; want old=1 new=%d", oldCalls, newCalls, wantNew)
				}
			})
		}
	}
}
