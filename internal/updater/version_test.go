package updater

import "testing"

func TestCurrentVersion_Default(t *testing.T) {
	orig := Version
	Version = ""
	defer func() { Version = orig }()

	got := CurrentVersion()
	if got != "dev" {
		t.Errorf("CurrentVersion() = %q, want %q", got, "dev")
	}
}

func TestCurrentVersion_Set(t *testing.T) {
	orig := Version
	Version = "1.2.3"
	defer func() { Version = orig }()

	got := CurrentVersion()
	if got != "1.2.3" {
		t.Errorf("CurrentVersion() = %q, want %q", got, "1.2.3")
	}
}
