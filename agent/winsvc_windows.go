//go:build windows

package agent

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/windows/svc"
)

// IsService reports whether this process was started by the service control manager.
func IsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// RunService runs run as the Windows service of the agent. The context ends when Windows asks the service to stop
// or the machine shuts down.
func RunService(run func(ctx context.Context) error) error {
	return svc.Run(winServiceName, &svcHandler{run: run})
}

type svcHandler struct {
	run func(ctx context.Context) error
}

func (h *svcHandler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done: //nolint:errcheck // only the end matters here
				case <-time.After(20 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			st <- svc.Status{State: svc.StopPending}
			if err != nil && !errors.Is(err, context.Canceled) {
				return false, 1 // a failure: the recovery settings of the service start it again
			}
			return false, 0
		}
	}
}
