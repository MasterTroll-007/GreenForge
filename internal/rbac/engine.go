package rbac

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Engine evaluates RBAC policies based on SSH certificate extensions.
type Engine struct {
	roles map[string]*Role
}

// Role defines a set of permissions.
type Role struct {
	Name        string   `yaml:"name"`
	Permissions []string `yaml:"permissions"` // e.g. "vcs:*", "build:execute", "shell"
}

// Permission represents a checked permission.
type Permission struct {
	Resource string // e.g. "vcs", "build", "shell", "db"
	Action   string // e.g. "read", "write", "execute", "*"
}

// NewEngine creates an RBAC engine with the given roles.
func NewEngine(roles []*Role) *Engine {
	e := &Engine{
		roles: make(map[string]*Role),
	}
	for _, r := range roles {
		e.roles[r.Name] = r
	}
	return e
}

// DefaultRoles returns the built-in role definitions.
func DefaultRoles() []*Role {
	return []*Role{
		{
			Name: "admin",
			Permissions: []string{"*"},
		},
		{
			Name: "developer",
			Permissions: []string{
				"vcs:*", "build:*", "shell", "db:read", "db:write",
				"analysis:*", "logs:read", "cicd:read", "cicd:trigger",
				"notify:send", "index:*",
			},
		},
		{
			Name: "viewer",
			Permissions: []string{
				"vcs:read", "logs:read", "cicd:read", "audit:read",
				"index:read",
			},
		},
	}
}

// CheckCert extracts the role from an SSH certificate and checks a permission.
func (e *Engine) CheckCert(cert *ssh.Certificate, perm Permission) error {
	roleName, ok := cert.Permissions.Extensions["greenforge-role@greenforge.dev"]
	if !ok {
		return fmt.Errorf("certificate has no greenforge-role extension")
	}
	return e.Check(roleName, perm)
}

// Check verifies if a role has a given permission.
func (e *Engine) Check(roleName string, perm Permission) error {
	role, exists := e.roles[roleName]
	if !exists {
		return fmt.Errorf("unknown role: %s", roleName)
	}

	permStr := perm.String()
	for _, p := range role.Permissions {
		if matchPermission(p, permStr) {
			return nil
		}
	}

	return fmt.Errorf("role %q does not have permission %q", roleName, permStr)
}

// CheckTools verifies if a cert has access to specific tools.
func (e *Engine) CheckTools(cert *ssh.Certificate, toolName string) error {
	// Check if device cert has tool restrictions
	allowedTools, ok := cert.Permissions.Extensions["greenforge-tools@greenforge.dev"]
	if ok {
		tools := strings.Split(allowedTools, ",")
		for _, t := range tools {
			if matchPermission(strings.TrimSpace(t), toolName) {
				return nil
			}
		}
		return fmt.Errorf("tool %q not allowed for this device certificate", toolName)
	}

	// No tool restriction → all tools allowed (subject to role permissions)
	return nil
}

// CheckSecrets verifies if a cert can access specific secrets.
func (e *Engine) CheckSecrets(cert *ssh.Certificate, secretName string) error {
	allowedSecrets, ok := cert.Permissions.Extensions["greenforge-secrets@greenforge.dev"]
	if !ok {
		// Check role
		roleName := cert.Permissions.Extensions["greenforge-role@greenforge.dev"]
		if roleName == "admin" {
			return nil
		}
		return fmt.Errorf("no secret access defined for certificate")
	}

	secrets := strings.Split(allowedSecrets, ",")
	for _, s := range secrets {
		if strings.TrimSpace(s) == secretName || strings.TrimSpace(s) == "*" {
			return nil
		}
	}
	return fmt.Errorf("secret %q not allowed for this certificate", secretName)
}

// GetRole returns the role for a given name.
func (e *Engine) GetRole(name string) (*Role, bool) {
	r, ok := e.roles[name]
	return r, ok
}

// ListRoles returns all defined roles.
func (e *Engine) ListRoles() []*Role {
	roles := make([]*Role, 0, len(e.roles))
	for _, r := range e.roles {
		roles = append(roles, r)
	}
	return roles
}

func (p Permission) String() string {
	if p.Action == "" || p.Action == "*" {
		return p.Resource
	}
	return p.Resource + ":" + p.Action
}

// matchPermission checks if a policy permission matches the requested permission.
// Supports wildcards: "*" matches everything, "vcs:*" matches "vcs:read", "vcs:write", etc.
func matchPermission(policy, requested string) bool {
	if policy == "*" {
		return true
	}

	policyParts := strings.SplitN(policy, ":", 2)
	requestedParts := strings.SplitN(requested, ":", 2)

	// Resource must match
	if policyParts[0] != requestedParts[0] {
		return false
	}

	// If policy has no action part, it matches all actions for that resource
	if len(policyParts) == 1 {
		return true
	}

	// Wildcard action
	if policyParts[1] == "*" {
		return true
	}

	// If requested has no action, policy with specific action won't match
	if len(requestedParts) == 1 {
		return true
	}

	return policyParts[1] == requestedParts[1]
}
