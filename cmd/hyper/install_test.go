package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptVerifiesAndInstallsRelease(t *testing.T) {
	fixtureDir, installDir, environment := installerFixture(t, "Linux", "x86_64")
	archiveName := "hyper_linux_amd64.tar.gz"
	archivePath := filepath.Join(fixtureDir, archiveName)
	writeArchive(t, archivePath, "release binary")
	writeChecksum(t, fixtureDir, archiveName, archivePath)

	command := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "install.sh"))
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}

	installed, err := os.ReadFile(filepath.Join(installDir, "hyper"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "release binary" {
		t.Fatalf("installed content = %q, want release binary", installed)
	}
	if !strings.Contains(string(output), "Installed hyper") {
		t.Fatalf("output = %q, want installation confirmation", output)
	}
}

func TestInstallScriptRejectsChecksumMismatch(t *testing.T) {
	fixtureDir, installDir, environment := installerFixture(t, "Darwin", "arm64")
	archiveName := "hyper_darwin_arm64.tar.gz"
	archivePath := filepath.Join(fixtureDir, archiveName)
	writeArchive(t, archivePath, "release binary")
	if err := os.WriteFile(filepath.Join(fixtureDir, "hyper_checksums.txt"), []byte(strings.Repeat("0", 64)+"  "+archiveName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "install.sh"))
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh succeeded with invalid checksum: %s", output)
	}
	if !strings.Contains(string(output), "checksum verification failed") {
		t.Fatalf("output = %q, want checksum failure", output)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "hyper")); !os.IsNotExist(statErr) {
		t.Fatalf("installed binary exists after checksum failure: %v", statErr)
	}
}

func TestInstallScriptRejectsUnsupportedPlatform(t *testing.T) {
	_, _, environment := installerFixture(t, "Darwin", "i386")
	command := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "install.sh"))
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh succeeded for unsupported platform: %s", output)
	}
	if !strings.Contains(string(output), "unsupported platform: darwin/386") {
		t.Fatalf("output = %q, want unsupported platform error", output)
	}
}

func installerFixture(t *testing.T, operatingSystem, architecture string) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	fixtureDir := filepath.Join(root, "fixtures")
	installDir := filepath.Join(root, "install")
	stubDir := filepath.Join(root, "bin")
	for _, directory := range []string{fixtureDir, stubDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(stubDir, "uname"), `#!/bin/sh
case "$1" in
  -s) printf '%s\n' "$HYPER_TEST_OS" ;;
  -m) printf '%s\n' "$HYPER_TEST_ARCH" ;;
esac
`)
	writeExecutable(t, filepath.Join(stubDir, "curl"), `#!/bin/sh
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
cp "$HYPER_INSTALL_FIXTURES/$(basename "$url")" "$output"
`)

	return fixtureDir, installDir, append(os.Environ(),
		"HOME="+root,
		"HYPER_INSTALL_DIR="+installDir,
		"HYPER_INSTALL_FIXTURES="+fixtureDir,
		"HYPER_TEST_ARCH="+architecture,
		"HYPER_TEST_OS="+operatingSystem,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+root,
	)
}

func writeArchive(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "hyper", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tarWriter, content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeChecksum(t *testing.T, fixtureDir, archiveName, archivePath string) {
	t.Helper()
	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(content), archiveName)
	if err := os.WriteFile(filepath.Join(fixtureDir, "hyper_checksums.txt"), []byte(checksum), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}
