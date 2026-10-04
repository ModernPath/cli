package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- connect / status

var factoryConnectSystem int

// systemLookup reads a bound system's identity from the server.
type systemLookup func(id int) (name, slug string, err error)

// resolveBinding sets the system id and API URL on cfg. A systemID of 0 means
// "keep the system this workspace is already bound to", which is what lets a
// re-run repair an existing config instead of demanding a re-init.
func resolveBinding(cfg *config.Config, systemID int, apiURLFlag string) error {
	if systemID == 0 && cfg.SystemID == 0 {
		return fmt.Errorf("usage: modernpath factory connect --system <id> [--api-url <url>]")
	}
	if systemID != 0 && systemID != cfg.SystemID {
		// a different system: drop the old identity rather than let the new id
		// wear the old name if the lookup below cannot reach the server
		cfg.SystemID = systemID
		cfg.SystemName, cfg.SystemSlug = "", ""
	}
	if apiURLFlag != "" {
		if cfg.APIURL != "" && apiURLFlag != cfg.APIURL {
			// a different server: the recorded identity was read from the OLD
			// one, and the same numeric id on another host is another system
			cfg.SystemName, cfg.SystemSlug = "", ""
		}
		cfg.APIURL = apiURLFlag
	}
	if cfg.APIURL == "" {
		cfg.APIURL = config.LocalAPIURL
	}
	return nil
}

// recordSystemIdentity fills in the system's name and slug from the server.
// Its error is advisory: the binding is written either way, because binding a
// workspace must not require the network.
func recordSystemIdentity(cfg *config.Config, lookup systemLookup) error {
	if lookup == nil {
		return nil
	}
	name, slug, err := lookup(cfg.SystemID)
	if err != nil {
		return err // keep whatever was already recorded for this same system
	}
	cfg.SystemName, cfg.SystemSlug = name, slug
	return nil
}

// connectResult reports what a connect achieved beyond binding.
type connectResult struct {
	// IdentityErr is advisory: the binding is saved whether or not the
	// system's name and slug could be read.
	IdentityErr error
}

// connectWorkspace binds, persists, and records the system's identity.
func connectWorkspace(cfg *config.Config, systemID int, apiURLFlag string,
	save func(*config.Config) error, lookup systemLookup) (connectResult, error) {

	if err := resolveBinding(cfg, systemID, apiURLFlag); err != nil {
		return connectResult{}, err
	}
	// Save the binding before looking anything up: the lookup authenticates
	// with credentials it reads back from this workspace's own config, so on a
	// first connect it cannot succeed until the config exists.
	if err := save(cfg); err != nil {
		return connectResult{}, err
	}
	res := connectResult{IdentityErr: recordSystemIdentity(cfg, lookup)}
	if res.IdentityErr != nil {
		return res, nil
	}
	return res, save(cfg)
}

var factoryConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Bind this workspace to a System (writes .modernpath/config.json)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.ReadConfig()
		if cfg == nil {
			cfg = &config.Config{}
		}
		res, err := connectWorkspace(cfg, factoryConnectSystem, apiURL, config.WriteConfig, serverSystemLookup())
		if err != nil {
			return err
		}
		// The written path, not a relative literal: run from a subdirectory,
		// ".modernpath/config.json" reads as "here" when the binding it updated
		// is a level or more up.
		printSuccess("connected: system %d via %s (%s)", cfg.SystemID, cfg.APIURL, connectedConfigPath())
		if res.IdentityErr != nil {
			printWarning("could not read the system's name and slug: %v\n  the binding is written — %s\n", res.IdentityErr, identityRepairHint(res.IdentityErr))
		}
		return nil
	},
}

// identityRepairHint names the step that can actually fix the failed identity
// lookup. Re-running connect repairs a reachability failure; a credential
// failure re-fails identically until the credential itself is fixed, and
// pointing the reader at a retry loop hides the real repair.
func identityRepairHint(err error) string {
	var ce credentialError
	if errors.As(err, &ce) {
		return fmt.Sprintf("run '%s', then re-run 'modernpath factory connect'", ce.repair)
	}
	return "re-run 'modernpath factory connect' once the server is reachable"
}

// connectedConfigPath names the binding that was just written, relative to the
// working directory when that is shorter to read than the absolute path.
func connectedConfigPath() string {
	dir, err := config.FindConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(config.ConfigDir, config.ConfigFile)
	}
	full := filepath.Join(dir, config.ConfigFile)
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, full); err == nil && len(rel) < len(full) {
			return rel
		}
	}
	return full
}

// serverSystemLookup reads identity through the factory lane's bearer.
func serverSystemLookup() systemLookup {
	return func(id int) (string, string, error) {
		env, err := factoryEnvLoad()
		if err != nil {
			return "", "", err
		}
		status, body, err := env.call("GET", fmt.Sprintf("/api/systems/%d", id), nil)
		if err != nil {
			return "", "", err
		}
		if status != 200 {
			return "", "", fmt.Errorf("server %d reading system %d", status, id)
		}
		name, _ := body["name"].(string)
		slug, _ := body["slug"].(string)
		if slug == "" {
			return "", "", fmt.Errorf("system %d returned no slug", id)
		}
		return name, slug, nil
	}
}
