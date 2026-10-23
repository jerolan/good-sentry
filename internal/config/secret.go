package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func loadSecret(configured, authMode, uploadDir string) (string, error) {
	if configured == "" {
		if authMode == "required" {
			return "", errors.New("SECRET_KEY is required when GOODSENTRY_AUTH=required")
		}
		return localSecret(filepath.Join(uploadDir, ".goodsentry-secret"))
	}
	if len(configured) < 32 || strings.Contains(strings.ToLower(configured), "change_me") {
		return "", fmt.Errorf("SECRET_KEY must contain at least 32 characters and cannot be a placeholder")
	}
	return configured, nil
}

func localSecret(path string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create local secret directory: %w", err)
	}
	if secret, err := readLocalSecret(path); err == nil {
		return secret, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate local SECRET_KEY: %w", err)
	}
	secret := hex.EncodeToString(random)
	temporary, err := os.CreateTemp(dir, ".goodsentry-secret-*")
	if err != nil {
		return "", fmt.Errorf("create local SECRET_KEY file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("protect local SECRET_KEY file: %w", err)
	}
	if _, err := temporary.WriteString(secret); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write local SECRET_KEY file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync local SECRET_KEY file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close local SECRET_KEY file: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("persist local SECRET_KEY file: %w", err)
		}
		return readLocalSecret(path)
	}
	return secret, nil
}

func readLocalSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read local SECRET_KEY file: %w", err)
	}
	secret := string(data)
	decoded, err := hex.DecodeString(secret)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != secret {
		return "", fmt.Errorf("local SECRET_KEY file is invalid; restore it or configure SECRET_KEY")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("protect local SECRET_KEY file: %w", err)
	}
	return secret, nil
}
