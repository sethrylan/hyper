# hyper

[![CI](https://github.com/sethrylan/hyper/workflows/CI/badge.svg)](https://github.com/sethrylan/hyper/actions)

A terminal UI for your GitHub work queue: important notifications, open PRs, and open issues, grouped by repo and updated in real time.

![demo](docs/demo.gif)

## Install

Release installs are recommended: they check for stable updates in the background and install them for the next launch.

```sh
curl -fsSL https://github.com/sethrylan/hyper/releases/latest/download/install.sh | sh
```

The installer detects macOS or Linux and installs the verified binary to `~/.local/bin`. If that directory is not on your `PATH`, add it to your shell configuration:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Set `HYPER_INSTALL_DIR` on the `sh` command to choose another user-writable directory:

```sh
curl -fsSL https://github.com/sethrylan/hyper/releases/latest/download/install.sh | HYPER_INSTALL_DIR="$HOME/bin" sh
```

To install manually, download the archive for your platform from the [latest release](https://github.com/sethrylan/hyper/releases/latest), verify it against `hyper_checksums.txt`, extract `hyper`, and move it to a user-writable directory on your `PATH`. Release archives are available for:

| Platform | Architectures |
|----------|---------------|
| macOS | Apple Silicon (`arm64`), Intel (`amd64`) |
| Linux | `arm64`, `amd64`, `386` |

For example, on Apple Silicon:

```sh
curl -LO https://github.com/sethrylan/hyper/releases/latest/download/hyper_darwin_arm64.tar.gz
curl -LO https://github.com/sethrylan/hyper/releases/latest/download/hyper_checksums.txt
grep ' hyper_darwin_arm64.tar.gz$' hyper_checksums.txt | shasum -a 256 -c -
tar -xzf hyper_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 hyper "$HOME/.local/bin/hyper"
```

You can opt out of automatic checks for a launch with `HYPER_NO_UPDATE=1 hyper`.

Alternatively, install from source with Go:

```sh
go install github.com/sethrylan/hyper/cmd/hyper@latest
```

Source installs do not auto-update; rerun the command to upgrade. Check any installation with `hyper --version`.

Requires [GitHub CLI](https://cli.github.com/) authentication:

```sh
gh auth login
```

## Use

```sh
hyper
```

| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Switch feed |
| `j` / `k` | Move down / up |
| `l` / `h` | Expand / collapse group |
| `o` / `enter` | Open in browser |
| `y` | Copy URL |
| `E` | Mark done (Important Notifications) |
| `r` | Refresh |
| `?` | Help |
| `q` | Quit |
