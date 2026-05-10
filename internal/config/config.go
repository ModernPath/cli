package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	ConfigDir     = ".modernpath"
	ConfigFile    = "config.json"
	AuthFile      = "auth.json"
	DefaultAPIURL = "https://beta.modernpath.ai"
	LocalAPIURL   = "http://localhost:4000"
)

// Config holds the project-level configuration
type Config struct {
	APIURL         string `json:"api_url"`
	SystemID       int    `json:"system_id,omitempty"`
	SystemName     string `json:"system_name,omitempty"`
	SystemSlug     string `json:"system_slug,omitempty"`
	InitiativeID   int    `json:"initiative_id,omitempty"`
	InitiativeName string `json:"initiative_name,omitempty"`
	LastSyncAt     string `json:"last_sync_at,omitempty"`
	AutoSync       bool   `json:"auto_sync,omitempty"`
}

// legacyConfig is used only for reading old config files that use architecture_* keys.
type legacyConfig struct {
	APIURL           string `json:"api_url"`
	ArchitectureID   int    `json:"architecture_id,omitempty"`
	ArchitectureName string `json:"architecture_name,omitempty"`
	ArchitectureSlug string `json:"architecture_slug,omitempty"`
	SystemID         int    `json:"system_id,omitempty"`
	SystemName       string `json:"system_name,omitempty"`
	SystemSlug       string `json:"system_slug,omitempty"`
	InitiativeID     int    `json:"initiative_id,omitempty"`
	InitiativeName   string `json:"initiative_name,omitempty"`
	LastSyncAt       string `json:"last_sync_at,omitempty"`
	AutoSync         bool   `json:"auto_sync,omitempty"`
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
		// Create .gitignore for auth.json
		gitignore := filepath.Join(localConfigDir, ".gitignore")
		if _, err := os.Stat(gitignore); os.IsNotExist(err) {
			content := "# ModernPath CLI - ignore sensitive files\nauth.json\n*.log\n"
			os.WriteFile(gitignore, []byte(content), 0644)
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
// Supports backward compatibility: if the file uses old architecture_* keys,
// those values are migrated into the new system_* fields.
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
		InitiativeID:   legacy.InitiativeID,
		InitiativeName: legacy.InitiativeName,
		LastSyncAt:     legacy.LastSyncAt,
		AutoSync:       legacy.AutoSync,
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

	return config, nil
}

// WriteConfig writes the configuration file
func WriteConfig(config *Config) error {
	configDir, err := GetConfigDir(true)
	if err != nil {
		return err
	}

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

// WriteAuth writes authentication credentials to .modernpath in the current directory
func WriteAuth(auth *Auth) error {
	configDir, err := GetConfigDir(true)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, AuthFile), data, 0600)
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

// GetDocsPath returns the path to the markdown docs directory
func GetDocsPath() (string, error) {
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

	docsPath := filepath.Join(configDir, "docs", config.SystemSlug)
	return docsPath, nil
}
