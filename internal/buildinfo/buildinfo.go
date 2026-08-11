//nolint:gochecknoglobals // GoReleaser injects these values with linker flags.
package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const (
	developmentVersion  = "dev"
	releaseDistribution = "release"
	sourceDistribution  = "source"
)

var (
	version      = developmentVersion
	distribution = sourceDistribution
)

// Distribution reports how the running binary was built.
func Distribution() string {
	if distribution == releaseDistribution {
		return releaseDistribution
	}
	return sourceDistribution
}

// IsRelease reports whether the binary came from a Hyper GitHub release.
func IsRelease() bool {
	return Distribution() == releaseDistribution && Version() != developmentVersion
}

// String returns the user-facing build description.
func String() string {
	return fmt.Sprintf("hyper %s (%s)", Version(), Distribution())
}

// Version returns the injected release version or the Go module version when available.
func Version() string {
	if normalized := normalizeVersion(version); normalized != developmentVersion {
		return normalized
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return developmentVersion
	}
	return resolveVersion(info.Main.Version)
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(devel)" || value == developmentVersion {
		return developmentVersion
	}
	return strings.TrimPrefix(value, "v")
}

func resolveVersion(moduleVersion string) string {
	return normalizeVersion(moduleVersion)
}
