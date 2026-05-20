//nolint:revive // Internal package exports are shared across command and tests.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"runtime"
)

func Copy(value string) error {
	if value == "" {
		return errors.New("nothing to copy")
	}
	if os.Getenv("TERM") != "dumb" {
		if _, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64(value)); err != nil {
			return fmt.Errorf("copy with OSC 52: %w", err)
		}
		return nil
	}
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.CommandContext(context.Background(), "pbcopy")
		cmd.Stdin = bytes.NewBufferString(value)
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

func base64(value string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(value)
	out := make([]byte, 0, (len(src)+2)/3*4)
	for i := 0; i < len(src); i += 3 {
		var chunk uint32
		remaining := len(src) - i
		chunk |= uint32(src[i]) << 16
		if remaining > 1 {
			chunk |= uint32(src[i+1]) << 8
		}
		if remaining > 2 {
			chunk |= uint32(src[i+2])
		}
		out = append(out, alphabet[(chunk>>18)&0x3f], alphabet[(chunk>>12)&0x3f])
		if remaining > 1 {
			out = append(out, alphabet[(chunk>>6)&0x3f])
		} else {
			out = append(out, '=')
		}
		if remaining > 2 {
			out = append(out, alphabet[chunk&0x3f])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}
