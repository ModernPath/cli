package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ConfigDir  = ".modernpath"
	ConfigFile = "config.json"
	AuthFile   = "auth.json"
	// DefaultAPIURL is the server a workspace talks to before it is bound, and
	// the "production" environment. It is the cloud production API host — the
	// same value as zitadel.ProdProfile.APIURL (locked by a test), not the SPA
	// host cloud.modernpath.ai, which answers every path with index.html. Beta
	// moved to BetaAPIURL when it began being decommissioned.
	DefaultAPIURL = "https://api.modernpath.ai"
	LocalAPIURL   = "http://localhost:4000"
	// BetaAPIURL is the legacy beta server, being decommissioned during the
	// beta→cloud migration. Named so `modernpath env` still recognises a
	// workspace pointed at it instead of labelling it "custom".
	BetaAPIURL = "https://beta.modernpath.ai"
)

// Config holds the project-level configuration
type Config struct {
	APIURL           string            `json:"api_url"`
	SystemID         int               `json:"system_id,omitempty"`
	SystemName       string            `json:"system_name,omitempty"`
	SystemSlug       string            `json:"system_slug,omitempty"`
	InitMode         string            `json:"init_mode,omitempty"`
	WorkspaceMembers []WorkspaceMember `json:"workspace_members,omitempty"`
	EpicID           int               `json:"epic_id,omitempty"`
	EpicName         string            `json:"epic_name,omitempty"`
	EpicSpecsDir     string            `json:"epic_specs_dir,omitempty"`
	LastSyncAt       string            `json:"last_sync_at,omitempty"`
	AutoSync         bool              `json:"auto_sync,omitempty"`
	// CurrentRelease is the workspace-level release stamp factory sync sends
	// in the batch envelope (REQ-CROSS-017). Local machine state — the tracked
	// source of truth is process/releases.md. Set via `factory release use`.
	CurrentRelease string `json:"current_release,omitempty"`
}

// WorkspaceMember binds a platform workspace member slug to a local checkout folder.
type WorkspaceMember struct {
	Slug        string `json:"slug"`
	FullName    string `json:"full_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
}

// legacyConfig is used only for reading old config files that use architecture_*
// or initiative_* keys.
type legacyConfig struct {
	APIURL             string `json:"api_url"`
	ArchitectureID     int    `json:"architecture_id,omitempty"`
	ArchitectureName   string `json:"architecture_name,omitempty"`
	ArchitectureSlug   string `json:"architecture_slug,omitempty"`
	SystemID           int    `json:"system_id,omitempty"`
	SystemName         string `json:"system_name,omitempty"`
	SystemSlug         string `json:"system_slug,omitempty"`
	InitiativeID       int    `json:"initiative_id,omitempty"`
	InitiativeName     string `json:"initiative_name,omitempty"`
	InitiativeSpecsDir string `json:"initiative_specs_dir,omitempty"`
	EpicID             int    `json:"epic_id,omitempty"`
	EpicName           string `json:"epic_name,omitempty"`
	EpicSpecsDir       string `json:"epic_specs_dir,omitempty"`
	LastSyncAt         string `json:"last_sync_at,omitempty"`
	AutoSync           bool   `json:"auto_sync,omitempty"`
	CurrentRelease     string `json:"current_release,omitempty"`
}

// Auth holds authentication credentials
type Auth struct {
	Token        string `json:"token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// FindConfigDir searches for .modernpath directory starting from cwd going up
func FindConfigDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		configPath := filepath.Join(dir, ConfigDir)
		if info, err := os.Stat(configPath); err == nil && info.IsDir() {
			return configPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", nil // Not found, but not an error
}

// GetConfigDir returns the config directory path, creating it if requested.
// When create=true, it ensures .modernpath exists in the current directory.
// When create=false, it searches upward for an existing .modernpath directory.
func GetConfigDir(create bool) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	localConfigDir := filepath.Join(cwd, ConfigDir)

	if create {
		// When creating, always use the current directory
		if err := os.MkdirAll(localConfigDir, 0755); err != nil {
			return "", err
		}
		if err := ensureSensitiveIgnore(localConfigDir); err != nil {
			return "", err
		}
		return localConfigDir, nil
	}

	// When not creating, search upward for existing config
	existing, err := FindConfigDir()
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	// Return local path even if it doesn't exist (caller will handle)
	return localConfigDir, nil
}

// ReadConfig reads the configuration file.
// Supports backward compatibility for the previous architecture_* system and
// initiative_* planning keys. Writes use only the canonical names.
func ReadConfig() (*Config, error) {
	configDir, err := FindConfigDir()
	if err != nil {
		return nil, err
	}
	if configDir == "" {
		return &Config{APIURL: DefaultAPIURL}, nil
	}

	configPath := filepath.Join(configDir, ConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{APIURL: DefaultAPIURL}, nil
		}
		return nil, err
	}

	// Parse into the legacy struct that has both old and new keys
	var legacy legacyConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	config := &Config{
		APIURL:         legacy.APIURL,
		LastSyncAt:     legacy.LastSyncAt,
		AutoSync:       legacy.AutoSync,
		CurrentRelease: legacy.CurrentRelease,
	}
	// Use new epic_* keys if present, otherwise fall back to old initiative_* keys.
	if legacy.EpicID > 0 {
		config.EpicID = legacy.EpicID
	} else if legacy.InitiativeID > 0 {
		config.EpicID = legacy.InitiativeID
	}

	if legacy.EpicName != "" {
		config.EpicName = legacy.EpicName
	} else if legacy.InitiativeName != "" {
		config.EpicName = legacy.InitiativeName
	}

	if legacy.EpicSpecsDir != "" {
		config.EpicSpecsDir = legacy.EpicSpecsDir
	} else if legacy.InitiativeSpecsDir != "" {
		config.EpicSpecsDir = legacy.InitiativeSpecsDir
	}

	// Use new system_* keys if present, otherwise fall back to old architecture_* keys
	if legacy.SystemID > 0 {
		config.SystemID = legacy.SystemID
	} else if legacy.ArchitectureID > 0 {
		config.SystemID = legacy.ArchitectureID
	}

	if legacy.SystemName != "" {
		config.SystemName = legacy.SystemName
	} else if legacy.ArchitectureName != "" {
		config.SystemName = legacy.ArchitectureName
	}

	if legacy.SystemSlug != "" {
		config.SystemSlug = legacy.SystemSlug
	} else if legacy.ArchitectureSlug != "" {
		config.SystemSlug = legacy.ArchitectureSlug
	}

	if config.APIURL == "" {
		config.APIURL = DefaultAPIURL
	}

	config.EpicSpecsDir = NormalizeEpicWorkspaceRelPath(config.EpicSpecsDir)

	return config, nil
}

// WriteConfig updates the binding this workspace is actually using: the one
// ReadConfig would read, which may be in a parent directory.
//
// It used to write to the current directory unconditionally, while ReadConfig
// searched upward. Every read-modify-write command — connect, auth, docs sync,
// factory sync, env, work — therefore forked a nested .modernpath when run from
// a subdirectory, and that copy then shadowed the real binding for everything
// later in that subtree: no auth.json, no last_sync_at, no release.
// `modernpath init` is the one command that means "here"; it calls
// InitConfig.
func WriteConfig(config *Config) error {
	configDir, err := WorkspaceConfigDir()
	if err != nil {
		return err
	}
	return writeConfigTo(configDir, config)
}

// WorkspaceConfigDir is the .modernpath directory this workspace is bound
// through — the one ReadConfig reads — creating one in the current directory
// only when nothing is bound anywhere above.
//
// Every path under .modernpath resolves through here: the binding, the
// credential, the specs and tasks trees, and the docs export. They belong to the
// workspace, not to whichever directory a command was invoked from.
func WorkspaceConfigDir() (string, error) {
	existing, err := FindConfigDir()
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}
	return GetConfigDir(true)
}

// InitConfig binds the CURRENT directory, creating .modernpath if needed. This
// is `modernpath init`: initialize here, even inside an already-bound
// workspace.
func InitConfig(config *Config) error {
	configDir, err := GetConfigDir(true)
	if err != nil {
		return err
	}
	return writeConfigTo(configDir, config)
}

func writeConfigTo(configDir string, config *Config) error {
	config.EpicSpecsDir = NormalizeEpicWorkspaceRelPath(config.EpicSpecsDir)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, ConfigFile), data, 0644)
}

// ReadAuth reads authentication credentials
func ReadAuth() (*Auth, error) {
	configDir, err := FindConfigDir()
	if err != nil {
		return nil, err
	}
	if configDir == "" {
		return &Auth{}, nil
	}

	authPath := filepath.Join(configDir, AuthFile)
	data, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Auth{}, nil
		}
		return nil, err
	}

	var auth Auth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

// WriteAuth writes authentication credentials into the bound workspace's
// .modernpath directory.
func WriteAuth(auth *Auth) error {
	configDir, err := WorkspaceConfigDir()
	if err != nil {
		return err
	}
	// The bound directory can predate the ignore rule — created by an older
	// tool, or existing only to hold tracked content. The write that stores
	// the secret provisions the protection alongside it, and a protection
	// that cannot be written means the secret is not stored: an unignored
	// token is one `git add` from a remote.
	if err := ensureSensitiveIgnore(configDir); err != nil {
		return fmt.Errorf("refusing to store credentials: cannot make %s ignore them: %w",
			filepath.Join(configDir, ".gitignore"), err)
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, AuthFile), data, 0600)
}

// ensureSensitiveIgnore makes .modernpath/.gitignore cover the credential
// file. A missing file is provisioned with the full default; an existing file
// is the user's — it keeps its content and gains only the auth.json rule the
// credential write depends on. Existing-but-incomplete was the gap that let
// WriteAuth store an unignored token.
func ensureSensitiveIgnore(configDir string) error {
	gitignore := filepath.Join(configDir, ".gitignore")
	existing, err := os.ReadFile(gitignore)
	if os.IsNotExist(err) {
		content := "# ModernPath CLI - ignore sensitive files\nauth.json\n*.log\n"
		return os.WriteFile(gitignore, []byte(content), 0644)
	}
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == "auth.json" {
			return nil
		}
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(gitignore, []byte(content+"auth.json\n"), 0644)
}

// IsInitialized checks if the current directory is an initialized modernpath project
func IsInitialized() bool {
	config, err := ReadConfig()
	if err != nil {
		return false
	}
	return config.SystemID > 0
}

// GetDatabasePath returns the path to the SQLite database
func GetDatabasePath() (string, error) {
	configDir, err := FindConfigDir()
	if err != nil || configDir == "" {
		return "", err
	}

	config, err := ReadConfig()
	if err != nil {
		return "", err
	}

	if config.SystemSlug == "" {
		return "", nil
	}

	dbPath := filepath.Join(configDir, config.SystemSlug+".sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", nil
	}

	return dbPath, nil
}

// The export root is not derivable from config: `factory connect` records no
// system_slug, and exports extract to the zip's own top-level folder under
// .modernpath/ rather than to docs/<slug>. Callers discover it on disk with
// cmd.listSystemExportSlugs / cmd.resolveSystemRootDir (REQ-CROSS-096).
