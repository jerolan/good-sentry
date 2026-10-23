package spanarchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Objects stores immutable archive objects. Keys are slash-separated relative
// names; implementations reject traversal and enforce the common size bound.
type Objects interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type LocalObjects struct {
	root string
}

func NewLocalObjects(root string) (*LocalObjects, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("span archive directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve span archive directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create span archive directory: %w", err)
	}
	return &LocalObjects{root: absolute}, nil
}

func (s *LocalObjects) Put(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return errors.New("span archive object size must be between 1 byte and 64 MiB")
	}
	destination, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if current, err := readLocalObject(ctx, destination); err == nil {
		if bytes.Equal(current, data) {
			return nil
		}
		return errors.New("span archive key already exists with different contents")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing span archive object: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create span archive object directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".goodsentry-archive-*.tmp")
	if err != nil {
		return fmt.Errorf("create span archive temporary object: %w", err)
	}
	tempName := temporary.Name()
	defer os.Remove(tempName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set span archive object permissions: %w", err)
	}
	remaining := data
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			_ = temporary.Close()
			return err
		}
		chunkSize := min(len(remaining), 1<<20)
		written, err := temporary.Write(remaining[:chunkSize])
		if err == nil && written != chunkSize {
			err = io.ErrShortWrite
		}
		if err != nil {
			_ = temporary.Close()
			return fmt.Errorf("write span archive temporary object: %w", err)
		}
		remaining = remaining[written:]
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync span archive temporary object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close span archive temporary object: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A hard link publishes the complete, fsynced file atomically and refuses to
	// replace a different object if two retries race on the same deterministic key.
	if err := os.Link(tempName, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			current, readErr := readLocalObject(ctx, destination)
			if readErr == nil && bytes.Equal(current, data) {
				return nil
			}
			if readErr != nil {
				return fmt.Errorf("read concurrently published span archive object: %w", readErr)
			}
			return errors.New("span archive key concurrently published with different contents")
		}
		return fmt.Errorf("publish span archive object: %w", err)
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("open span archive object directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync span archive object directory: %w", err)
	}
	return nil
}

func (s *LocalObjects) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename, err := s.objectPath(key)
	if err != nil {
		return nil, err
	}
	return readLocalObject(ctx, filename)
}

func (s *LocalObjects) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	filename, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove span archive object: %w", err)
	}
	return nil
}

func (s *LocalObjects) objectPath(key string) (string, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	filename := filepath.Join(s.root, filepath.FromSlash(clean))
	relative, err := filepath.Rel(s.root, filename)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("span archive key escapes its directory")
	}
	return filename, nil
}

func readLocalObject(ctx context.Context, filename string) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > MaxObjectBytes {
		return nil, errors.New("span archive object must be a regular file from 1 byte to 64 MiB")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read span archive object: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return nil, errors.New("span archive object size must be between 1 byte and 64 MiB")
	}
	return data, nil
}

func cleanKey(key string) (string, error) {
	if key == "" || strings.ContainsAny(key, "\\\x00") || strings.HasPrefix(key, "/") {
		return "", errors.New("span archive key must be a relative slash-separated path")
	}
	clean := path.Clean(key)
	if clean == "." || clean != key || strings.HasPrefix(clean, "../") {
		return "", errors.New("span archive key contains an invalid path")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("span archive key contains an invalid path")
		}
	}
	return clean, nil
}
