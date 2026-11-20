//go:build !windows

// Package svcwin is Windows-only; this stub keeps the CLI compiling
// elsewhere with the same call shapes.
package svcwin

import (
	"context"
	"errors"
)

var errNotWindows = errors.New("windows services only exist on windows")

func IsService() bool { return false }

func RunService(onStart func() (func(ctx context.Context) error, error)) error {
	return errNotWindows
}

func Install(exePath, configPath string) error { return errNotWindows }

func Uninstall() error { return errNotWindows }

func RestartService() error { return errNotWindows }
