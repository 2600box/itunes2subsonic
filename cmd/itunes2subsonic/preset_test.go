package main

import "testing"

func TestApplyOverridesPresetDryRun(t *testing.T) {
	originalDryRun := *dryRun
	defer func() {
		*dryRun = originalDryRun
	}()

	*dryRun = true
	setFlags := map[string]bool{}
	applyDryRunOverrides(setFlags, true)

	p := preset{DryRun: boolPtr(true)}
	applyPreset(p, setFlags)

	if *dryRun {
		t.Fatalf("expected apply to keep dry_run disabled even with preset dry_run=true")
	}
}

func TestExplicitDryRunFalseOverridesPreset(t *testing.T) {
	originalDryRun := *dryRun
	defer func() {
		*dryRun = originalDryRun
	}()

	*dryRun = false
	setFlags := map[string]bool{"dry_run": true}
	p := preset{DryRun: boolPtr(true)}
	applyPreset(p, setFlags)

	if *dryRun {
		t.Fatalf("expected --dry_run=false to keep dry_run disabled even with preset dry_run=true")
	}
}

func boolPtr(value bool) *bool {
	return &value
}
