//go:build windows

package check

import (
	"context"
	"os/exec"
)

func execCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "cmd", "/C", command), nil
}
