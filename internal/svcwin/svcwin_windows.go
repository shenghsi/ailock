//go:build windows

// Package svcwin hosts the daemon inside a real Windows service so it can
// run at boot as LocalSystem (which also grants hosts-file write access).
package svcwin

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "ailock"

// IsService reports whether the current process was started by the Windows
// service controller.
func IsService() bool {
	is, err := svc.IsWindowsService()
	return err == nil && is
}

// RunService runs the daemon under the service controller (called instead
// of the foreground path when IsService() is true). onStart supplies the
// daemon config.
func RunService(onStart func() (runFn func(ctx context.Context) error, err error)) error {
	h := &handler{onStart: onStart}
	return svc.Run(serviceName, h)
}

type handler struct {
	onStart func() (func(ctx context.Context) error, error)
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	run, err := h.onStart()
	if err != nil {
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	// Give the fail-closed initial block a moment, then report running.
	time.Sleep(500 * time.Millisecond)
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				return true, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}

// Install registers the service pointing at exePath with the given config
// path argument, sets it to auto-start and starts it. Re-running it on an
// existing service replaces it, so `ailock install` doubles as the
// updater (stop → recreate → start; the caller replaces the exe first).
func Install(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager (need admin): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		if err := stopAndDelete(s); err != nil {
			return err
		}
	}
	s, err := m.CreateService(serviceName, exePath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: "ailock (AI kill switch)",
		Description: "Sinkholes US AI provider domains while the proxy is down.",
	}, "-c", configPath, "run")
	if err != nil {
		return err
	}
	defer s.Start()
	return nil
}

// stopAndDelete stops s (waiting briefly) and marks it for deletion.
func stopAndDelete(s *mgr.Service) error {
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		if _, err := s.Control(1); err != nil { // SERVICE_CONTROL_STOP
			return fmt.Errorf("stop service: %w", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			st, err := s.Query()
			if err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return s.Delete()
}

// Uninstall stops and removes the service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager (need admin): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found", serviceName)
	}
	return stopAndDelete(s)
}
