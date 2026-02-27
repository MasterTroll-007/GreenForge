// Package certsdk provides a client library for interacting with the GreenForge CA.
//
// This is used by tools and external services that need to verify certificates
// or request new certificates from the GreenForge Certificate Authority.
package certsdk

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Client provides access to the GreenForge CA for cert operations.
type Client struct {
	caDir string
}

// NewClient creates a new CA client.
func NewClient(caDir string) *Client {
	return &Client{caDir: caDir}
}

// UserCAPublicKey returns the user CA public key for cert verification.
func (c *Client) UserCAPublicKey() (ssh.PublicKey, error) {
	data, err := os.ReadFile(filepath.Join(c.caDir, "user_ca.pub"))
	if err != nil {
		return nil, fmt.Errorf("reading user CA public key: %w", err)
	}

	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing user CA public key: %w", err)
	}

	return key, nil
}

// HostCAPublicKey returns the host CA public key.
func (c *Client) HostCAPublicKey() (ssh.PublicKey, error) {
	data, err := os.ReadFile(filepath.Join(c.caDir, "host_ca.pub"))
	if err != nil {
		return nil, fmt.Errorf("reading host CA public key: %w", err)
	}

	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing host CA public key: %w", err)
	}

	return key, nil
}

// VerifyCert checks if a certificate was signed by the GreenForge CA.
func (c *Client) VerifyCert(certData []byte) (*ssh.Certificate, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey(certData)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}

	cert, ok := key.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("not a certificate")
	}

	caKey, err := c.UserCAPublicKey()
	if err != nil {
		return nil, err
	}

	checker := &ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			return ssh.KeysEqual(auth, caKey)
		},
	}

	if err := checker.CheckCert("greenforge", cert); err != nil {
		return nil, fmt.Errorf("certificate verification failed: %w", err)
	}

	return cert, nil
}

// GenerateKeyPair generates a new Ed25519 key pair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("generating key pair: %w", err)
	}
	return pub, priv, nil
}

// GetCertRole extracts the GreenForge role from a certificate.
func GetCertRole(cert *ssh.Certificate) string {
	if cert == nil {
		return ""
	}
	return cert.Permissions.Extensions["greenforge-role@greenforge.dev"]
}

// GetCertAllowedTools extracts allowed tools from a certificate.
func GetCertAllowedTools(cert *ssh.Certificate) []string {
	if cert == nil {
		return nil
	}
	tools, ok := cert.Permissions.Extensions["greenforge-tools@greenforge.dev"]
	if !ok {
		return nil
	}
	// Split by comma
	var result []string
	start := 0
	for i := 0; i <= len(tools); i++ {
		if i == len(tools) || tools[i] == ',' {
			t := tools[start:i]
			if len(t) > 0 {
				result = append(result, t)
			}
			start = i + 1
		}
	}
	return result
}
