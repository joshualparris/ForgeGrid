//go:build windows

package worker

import (
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// TestExecuteReportsRunningWithoutWaitingForRunFunc guards against a
// regression of the SCM start-timeout bug: main.go relies on Execute()
// reporting svc.Running (and therefore Windows' Service Control Manager
// considering the service started) without waiting for runFunc - which now
// carries the network-bound coordinator pairing - to complete.
func TestExecuteReportsRunningWithoutWaitingForRunFunc(t *testing.T) {
	unblock := make(chan struct{})
	started := make(chan struct{})
	svcImpl := &forgeGridService{runFunc: func() {
		close(started)
		<-unblock // simulate a slow/blocked coordinator pairing
	}}

	requests := make(chan svc.ChangeRequest)
	changes := make(chan svc.Status, 4)

	go svcImpl.Execute(nil, requests, changes)

	deadline := time.After(2 * time.Second)
	sawRunning := false
	for !sawRunning {
		select {
		case s := <-changes:
			if s.State == svc.Running {
				sawRunning = true
			}
		case <-deadline:
			t.Fatal("Execute did not report svc.Running promptly; runFunc must not block status reporting")
		}
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runFunc was never invoked after Running was reported")
	}

	close(unblock)
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
}
