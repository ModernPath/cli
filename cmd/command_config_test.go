package cmd

import (
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func TestCommandConfigUsesGlobalAPIURLOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := config.WriteConfig(&config.Config{APIURL: "https://configured.example", SystemID: 7}); err != nil {
		t.Fatal(err)
	}
	original := apiURL
	apiURL = "https://flag.example"
	t.Cleanup(func() { apiURL = original })

	cfg, err := commandConfig()
	if err != nil {
		t.Fatalf("commandConfig: %v", err)
	}
	if cfg.APIURL != "https://flag.example" {
		t.Fatalf("APIURL = %q, want global flag override", cfg.APIURL)
	}
}

func TestCLIBindingUsesGlobalAPIURLOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := config.WriteConfig(&config.Config{APIURL: "https://configured.example", SystemID: 7}); err != nil {
		t.Fatal(err)
	}
	original := apiURL
	apiURL = "https://flag.example"
	t.Cleanup(func() { apiURL = original })

	env, err := cliBindingLoad()
	if err != nil {
		t.Fatalf("cliBindingLoad: %v", err)
	}
	if env.APIURL != "https://flag.example" {
		t.Fatalf("APIURL = %q, want global flag override", env.APIURL)
	}
}
