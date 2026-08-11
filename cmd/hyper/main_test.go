package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHandleVersion(t *testing.T) {
	var output bytes.Buffer
	if !handleInformationalArgs([]string{"--version"}, &output) {
		t.Fatal("--version was not handled")
	}
	if got := output.String(); !strings.HasPrefix(got, "hyper ") || !strings.Contains(got, "(source)") {
		t.Fatalf("version output = %q, want source build description", got)
	}
}

func TestHandleInformationalArgsIgnoresOtherArguments(t *testing.T) {
	var output bytes.Buffer
	if handleInformationalArgs([]string{"--help"}, &output) {
		t.Fatal("--help was handled as an informational argument")
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestAutoUpdateEnabled(t *testing.T) {
	tests := []struct {
		name    string
		release bool
		optOut  string
		want    bool
	}{
		{name: "release", release: true, want: true},
		{name: "source", release: false, want: false},
		{name: "opted out", release: true, optOut: "1", want: false},
		{name: "zero does not opt out", release: true, optOut: "0", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := autoUpdateEnabled(test.release, test.optOut); got != test.want {
				t.Fatalf("autoUpdateEnabled(%t, %q) = %t, want %t", test.release, test.optOut, got, test.want)
			}
		})
	}
}
