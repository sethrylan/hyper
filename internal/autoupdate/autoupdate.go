//nolint:revive // Internal package exports are shared across command and TUI packages.
package autoupdate

import (
	"context"
	"errors"
	"fmt"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const checksumFilename = "hyper_checksums.txt"

type Candidate struct {
	version string
	release *selfupdate.Release
}

type Result struct {
	ApplyError     error
	UpdatedVersion string
}

type Service struct {
	backend        backend
	currentVersion string
	executablePath func() (string, error)
}

type backend interface {
	Apply(ctx context.Context, candidate Candidate, path string) error
	Latest(ctx context.Context, currentVersion string) (Candidate, bool, error)
}

type githubBackend struct {
	repository selfupdate.Repository
	updater    *selfupdate.Updater
}

func New(token, currentVersion string) (*Service, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{APIToken: token})
	if err != nil {
		return nil, fmt.Errorf("create GitHub release source: %w", err)
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: checksumFilename},
	})
	if err != nil {
		return nil, fmt.Errorf("create release updater: %w", err)
	}
	return &Service{
		backend: githubBackend{
			repository: selfupdate.NewRepositorySlug("sethrylan", "hyper"),
			updater:    updater,
		},
		currentVersion: currentVersion,
		executablePath: selfupdate.ExecutablePath,
	}, nil
}

func (s *Service) Update(ctx context.Context) Result {
	candidate, newer, err := s.backend.Latest(ctx, s.currentVersion)
	if err != nil || !newer {
		return Result{}
	}
	executable, err := s.executablePath()
	if err != nil {
		return Result{ApplyError: fmt.Errorf("locate executable: %w", err)}
	}
	if err := s.backend.Apply(ctx, candidate, executable); err != nil {
		return Result{ApplyError: fmt.Errorf("replace executable: %w", err)}
	}
	return Result{UpdatedVersion: candidate.version}
}

func (b githubBackend) Apply(ctx context.Context, candidate Candidate, path string) error {
	if candidate.release == nil {
		return errors.New("release metadata is missing")
	}
	return b.updater.UpdateTo(ctx, candidate.release, path)
}

func (b githubBackend) Latest(ctx context.Context, currentVersion string) (Candidate, bool, error) {
	release, found, err := b.updater.DetectLatest(ctx, b.repository)
	if err != nil || !found {
		return Candidate{}, false, err
	}
	if release.LessOrEqual(currentVersion) {
		return Candidate{}, false, nil
	}
	return Candidate{version: release.Version(), release: release}, true, nil
}
