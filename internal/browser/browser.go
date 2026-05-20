//nolint:revive // Internal package exports are shared across command and tests.
package browser

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

func Open(url string) error {
	if url == "" {
		return errors.New("no URL to open")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) //nolint:gosec // URL is passed as a single argument to the OS opener.
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // URL is passed as a single argument to the OS opener.
	default:
		cmd = exec.Command("xdg-open", url) //nolint:gosec // URL is passed as a single argument to the OS opener.
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}
