//nolint:revive // Internal package exports are shared across command and tests.
package clipboard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func Copy(value string) error {
	if value == "" {
		return errors.New("nothing to copy")
	}
	if os.Getenv("TERM") != "dumb" {
		if _, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(value))); err != nil {
			return fmt.Errorf("copy with OSC 52: %w", err)
		}
		return nil
	}
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.CommandContext(context.Background(), "pbcopy")
		cmd.Stdin = strings.NewReader(value)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pbcopy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("clipboard unavailable; URL: %s", value)
}

func CopyLink(url, title string) error {
	if url == "" {
		return errors.New("nothing to copy")
	}
	if title != "" && runtime.GOOS == "darwin" {
		if err := copyHTML(url, title); err == nil {
			return nil
		}
	}
	return Copy(url)
}

func copyHTML(url, title string) error {
	script := `
ObjC.import('AppKit')
ObjC.import('Foundation')

function run(argv) {
  const plainText = argv[0]
  const htmlText = argv[1]
  const pasteboard = $.NSPasteboard.generalPasteboard
  pasteboard.clearContents
  pasteboard.setStringForType($(plainText), $.NSPasteboardTypeString)
  pasteboard.setStringForType($(htmlText), $.NSPasteboardTypeHTML)
}
`
	cmd := exec.CommandContext(context.Background(), "osascript", "-l", "JavaScript", "-e", script, url, linkHTML(url, title)) //nolint:gosec // Arguments are passed directly to osascript, not interpreted by a shell.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copy rich link: %w", err)
	}
	return nil
}

func linkHTML(url, title string) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(url), html.EscapeString(title))
}
