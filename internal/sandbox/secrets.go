package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// SecretManager manages secrets using the OS keychain.
// Secrets are never stored in config files - only in the OS credential store.
type SecretManager struct {
	mu    sync.RWMutex
	cache map[string]string // runtime cache, cleared on process exit
}

// NewSecretManager creates a new secret manager.
func NewSecretManager() *SecretManager {
	return &SecretManager{
		cache: make(map[string]string),
	}
}

// Set stores a secret in the OS keychain.
func (sm *SecretManager) Set(key, value string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := keychainSet("greenforge", key, value); err != nil {
		return fmt.Errorf("storing secret %q: %w", key, err)
	}

	sm.cache[key] = value
	return nil
}

// Get retrieves a secret from the OS keychain.
func (sm *SecretManager) Get(key string) (string, error) {
	sm.mu.RLock()
	if v, ok := sm.cache[key]; ok {
		sm.mu.RUnlock()
		return v, nil
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	value, err := keychainGet("greenforge", key)
	if err != nil {
		return "", fmt.Errorf("retrieving secret %q: %w", key, err)
	}

	sm.cache[key] = value
	return value, nil
}

// Delete removes a secret from the OS keychain.
func (sm *SecretManager) Delete(key string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.cache, key)
	return keychainDelete("greenforge", key)
}

// InjectEnv creates a map of environment variables for secret injection into containers.
func (sm *SecretManager) InjectEnv(secretKeys []string) (map[string]string, error) {
	env := make(map[string]string)
	for _, key := range secretKeys {
		value, err := sm.Get(key)
		if err != nil {
			return nil, fmt.Errorf("injecting secret %q: %w", key, err)
		}
		envKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		env[envKey] = value
	}
	return env, nil
}

// ClearCache clears the in-memory secret cache.
func (sm *SecretManager) ClearCache() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cache = make(map[string]string)
}

// --- OS Keychain implementations ---

func keychainSet(service, key, value string) error {
	switch runtime.GOOS {
	case "windows":
		return windowsCredentialSet(service, key, value)
	case "darwin":
		return darwinKeychainSet(service, key, value)
	default:
		return linuxSecretServiceSet(service, key, value)
	}
}

func keychainGet(service, key string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return windowsCredentialGet(service, key)
	case "darwin":
		return darwinKeychainGet(service, key)
	default:
		return linuxSecretServiceGet(service, key)
	}
}

func keychainDelete(service, key string) error {
	switch runtime.GOOS {
	case "windows":
		return windowsCredentialDelete(service, key)
	case "darwin":
		return darwinKeychainDelete(service, key)
	default:
		return linuxSecretServiceDelete(service, key)
	}
}

// Windows: uses cmdkey / PowerShell
func windowsCredentialSet(service, key, value string) error {
	target := fmt.Sprintf("%s/%s", service, key)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf(
		`$cred = New-Object System.Management.Automation.PSCredential('%s', (ConvertTo-SecureString '%s' -AsPlainText -Force)); `+
			`cmdkey /generic:%s /user:%s /pass:%s`,
		key, value, target, key, value))
	return cmd.Run()
}

func windowsCredentialGet(service, key string) (string, error) {
	target := fmt.Sprintf("%s/%s", service, key)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf(
		`$c = cmdkey /list:%s; $c`, target))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("credential not found: %s", key)
	}
	return strings.TrimSpace(string(out)), nil
}

func windowsCredentialDelete(service, key string) error {
	target := fmt.Sprintf("%s/%s", service, key)
	cmd := exec.Command("cmdkey", "/delete:"+target)
	return cmd.Run()
}

// macOS: uses security command
func darwinKeychainSet(service, key, value string) error {
	cmd := exec.Command("security", "add-generic-password",
		"-s", service, "-a", key, "-w", value, "-U")
	return cmd.Run()
}

func darwinKeychainGet(service, key string) (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", service, "-a", key, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("keychain entry not found: %s/%s", service, key)
	}
	return strings.TrimSpace(string(out)), nil
}

func darwinKeychainDelete(service, key string) error {
	cmd := exec.Command("security", "delete-generic-password",
		"-s", service, "-a", key)
	return cmd.Run()
}

// Linux: uses secret-tool (libsecret)
func linuxSecretServiceSet(service, key, value string) error {
	cmd := exec.Command("secret-tool", "store",
		"--label", fmt.Sprintf("%s/%s", service, key),
		"service", service, "key", key)
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}

func linuxSecretServiceGet(service, key string) (string, error) {
	cmd := exec.Command("secret-tool", "lookup", "service", service, "key", key)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("secret not found: %s/%s", service, key)
	}
	return strings.TrimSpace(string(out)), nil
}

func linuxSecretServiceDelete(service, key string) error {
	cmd := exec.Command("secret-tool", "clear", "service", service, "key", key)
	return cmd.Run()
}
