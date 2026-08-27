//nolint:testpackage // These tests exercise the updater's internal boundary.
package autoupdate

import (
	"context"
	"errors"
	"testing"
)

type backendStub struct {
	applyErr  error
	applyPath string
	candidate Candidate
	latestErr error
	newer     bool
}

func (b *backendStub) Apply(_ context.Context, _ Candidate, path string) error {
	b.applyPath = path
	return b.applyErr
}

func (b *backendStub) Latest(context.Context, string) (Candidate, bool, error) {
	return b.candidate, b.newer, b.latestErr
}

func TestUpdateAppliesNewRelease(t *testing.T) {
	backend := &backendStub{candidate: Candidate{version: "1.2.3"}, newer: true}
	service := &Service{
		backend:        backend,
		currentVersion: "1.2.2",
		executablePath: func() (string, error) { return "/tmp/hyper", nil },
	}

	result := service.Update(t.Context())
	if result.ApplyError != nil {
		t.Fatal(result.ApplyError)
	}
	if result.UpdatedVersion != "1.2.3" {
		t.Fatalf("updated version = %q, want 1.2.3", result.UpdatedVersion)
	}
	if backend.applyPath != "/tmp/hyper" {
		t.Fatalf("apply path = %q, want /tmp/hyper", backend.applyPath)
	}
}

func TestUpdateIgnoresCheckFailure(t *testing.T) {
	service := &Service{
		backend:        &backendStub{latestErr: errors.New("offline")},
		currentVersion: "1.2.2",
		executablePath: func() (string, error) { return "/tmp/hyper", nil },
	}

	result := service.Update(t.Context())
	if result.ApplyError != nil || result.UpdatedVersion != "" {
		t.Fatalf("result = %#v, want empty result", result)
	}
}

func TestUpdateReportsApplyFailure(t *testing.T) {
	backend := &backendStub{
		applyErr:  errors.New("permission denied"),
		candidate: Candidate{version: "1.2.3"},
		newer:     true,
	}
	service := &Service{
		backend:        backend,
		currentVersion: "1.2.2",
		executablePath: func() (string, error) { return "/usr/local/bin/hyper", nil },
	}

	result := service.Update(t.Context())
	if result.ApplyError == nil {
		t.Fatal("ApplyError = nil, want replacement failure")
	}
	if result.UpdatedVersion != "" {
		t.Fatalf("updated version = %q after failure", result.UpdatedVersion)
	}
}

func TestUpdateReportsExecutableLookupFailure(t *testing.T) {
	service := &Service{
		backend:        &backendStub{candidate: Candidate{version: "1.2.3"}, newer: true},
		currentVersion: "1.2.2",
		executablePath: func() (string, error) { return "", errors.New("missing") },
	}

	result := service.Update(t.Context())
	if result.ApplyError == nil {
		t.Fatal("ApplyError = nil, want executable lookup failure")
	}
}
