package operations

import (
	"strings"
	"testing"
)

func TestConfigValidateRejectsUnknownDriver(t *testing.T) {
	cfg := Config{
		Instances: []Instance{
			{Name: "broken", Type: "thing", Driver: "garbage", Token: "x"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown driver, got nil")
	}
	if !strings.Contains(err.Error(), "garbage") {
		t.Errorf("error %q should mention the bad driver", err)
	}
}

func TestConfigValidateAcceptsAliases(t *testing.T) {
	cases := []Instance{
		{Name: "gitea", Type: "gitea", Driver: "forgejo"},
		{Name: "gitlab-mirror", Type: "gitlab-mirror", Driver: "gitlab"},
		{Name: "plain", Type: "gitlab"}, // no Driver — real instance
	}
	for _, inst := range cases {
		t.Run(inst.Name, func(t *testing.T) {
			cfg := Config{Instances: []Instance{inst}}
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestConfigAllPreservesDriver makes sure a valid alias entry flows through
// All() unchanged so the client builder can read Driver to pick the API
// client while keeping Type as the public label.
func TestConfigAllPreservesDriver(t *testing.T) {
	cfg := Config{
		Instances: []Instance{
			{Name: "gitea-home", Type: "gitea", Driver: "forgejo", Token: "x", URL: "https://gitea.example.com"},
		},
	}
	all := cfg.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(all))
	}
	got := all[0]
	if got.Type != "gitea" {
		t.Errorf("Type = %q, want %q", got.Type, "gitea")
	}
	if got.Driver != "forgejo" {
		t.Errorf("Driver = %q, want %q", got.Driver, "forgejo")
	}
	if got.Name != "gitea-home" {
		t.Errorf("Name = %q, want %q", got.Name, "gitea-home")
	}
}
