package winsvc

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

type WorkerRunner interface {
	Start()
}

type myService struct {
	runner WorkerRunner
}

func (m *myService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	
	m.runner.Start()
	
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			// Should ideally shutdown worker gracefully
			break loop
		default:
			log.Printf("unexpected control request #%d", c)
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func RunService(name string, runner WorkerRunner) error {
	return svc.Run(name, &myService{runner: runner})
}

func IsWindowsService() bool {
	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		return false
	}
	return !isInteractive
}
