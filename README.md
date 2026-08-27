# hyper

[![CI](https://github.com/sethrylan/hyper/workflows/CI/badge.svg)](https://github.com/sethrylan/hyper/actions)

A terminal UI for your GitHub work queue: important notifications, open PRs, and open issues, grouped by repo and updated in real time.

![demo](docs/demo.gif)

## Install

Release installs are recommended.

```sh
curl -fsSL https://github.com/sethrylan/hyper/releases/latest/download/install.sh | sh
```

Add `~/.local/bin` to your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
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
| `r` | Refresh active lane |
| `shift+r` | Show GitHub account rate limits |
| `?` | Help |
| `q` | Quit |

Hyper aims to keep its polling and query usage modest, but it does not enforce a separate local API budget. The status bar and rate-limit screen report limits returned by GitHub.
