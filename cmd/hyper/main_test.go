package main

import (
	"strings"
	"testing"

	"github.com/sethrylan/hyper/internal/buildinfo"
)

func TestHandleArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOutput string
		wantHandle bool
	}{
		{name: "short help", args: []string{"-h"}, wantOutput: usage, wantHandle: true},
		{name: "long help", args: []string{"--help"}, wantOutput: usage, wantHandle: true},
		{name: "version", args: []string{"--version"}, wantOutput: buildinfo.String() + "\n", wantHandle: true},
		{name: "no arguments"},
		{name: "unknown argument", args: []string{"--unknown"}},
		{name: "multiple arguments", args: []string{"--help", "extra"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			if got := handleArgs(test.args, &output); got != test.wantHandle {
				t.Fatalf("handleArgs() = %t, want %t", got, test.wantHandle)
			}
			if got := output.String(); got != test.wantOutput {
				t.Fatalf("output = %q, want %q", got, test.wantOutput)
			}
		})
	}
}
