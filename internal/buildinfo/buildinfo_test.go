//nolint:testpackage // These tests exercise linker-injected package state.
package buildinfo

import "testing"

func TestInjectedReleaseBuild(t *testing.T) {
	originalVersion, originalDistribution := version, distribution
	t.Cleanup(func() {
		version = originalVersion
		distribution = originalDistribution
	})

	version = "v1.2.3"
	distribution = releaseDistribution

	if got := Version(); got != "1.2.3" {
		t.Fatalf("Version() = %q, want 1.2.3", got)
	}
	if !IsRelease() {
		t.Fatal("IsRelease() = false, want true")
	}
	if got := String(); got != "hyper 1.2.3 (release)" {
		t.Fatalf("String() = %q, want release description", got)
	}
}

func TestSourceBuildNeverEnablesReleaseUpdates(t *testing.T) {
	originalVersion, originalDistribution := version, distribution
	t.Cleanup(func() {
		version = originalVersion
		distribution = originalDistribution
	})

	version = "1.2.3"
	distribution = sourceDistribution

	if IsRelease() {
		t.Fatal("IsRelease() = true for source build")
	}
	if got := String(); got != "hyper 1.2.3 (source)" {
		t.Fatalf("String() = %q, want source description", got)
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := map[string]string{
		"":        developmentVersion,
		"(devel)": developmentVersion,
		"dev":     developmentVersion,
		"v2.0.1":  "2.0.1",
		"2.0.1":   "2.0.1",
	}
	for input, want := range tests {
		if got := normalizeVersion(input); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}
