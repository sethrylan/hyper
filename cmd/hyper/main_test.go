package main

import (
	"testing"
)

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
