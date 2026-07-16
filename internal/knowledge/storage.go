package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type StoredObject struct {
	Key      string
	Size     int64
	Checksum string
}

type ReadSeekCloserAt interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

// ObjectStorage deliberately exposes keys instead of file paths. A future S3
// implementation can satisfy the same interface by materializing Open calls
// into a controlled temporary file.
type ObjectStorage interface {
	Put(ctx context.Context, extension string, source io.Reader, maxBytes int64) (StoredObject, error)
	Open(ctx context.Context, key string) (ReadSeekCloserAt, int64, error)
	Delete(ctx context.Context, key string) error
}

type LocalObjectStorage struct {
	root string
}

func NewLocalObjectStorage(root string) (*LocalObjectStorage, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("object storage root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object storage root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create object storage root: %w", err)
	}
	return &LocalObjectStorage{root: absolute}, nil
}

func (s *LocalObjectStorage) Put(ctx context.Context, extension string, source io.Reader, maxBytes int64) (StoredObject, error) {
	if source == nil || maxBytes <= 0 {
		return StoredObject{}, fmt.Errorf("source and positive max bytes are required")
	}
	extension = strings.ToLower(extension)
	if _, ok := SupportedFormats[extension]; !ok && extension != ".jpg" && extension != ".json" {
		return StoredObject{}, fmt.Errorf("unsupported extension")
	}
	key, err := randomStorageKey(extension)
	if err != nil {
		return StoredObject{}, fmt.Errorf("generate storage key: %w", err)
	}
	target, err := s.resolve(key)
	if err != nil {
		return StoredObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return StoredObject{}, fmt.Errorf("create object directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return StoredObject{}, fmt.Errorf("create temporary object: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return StoredObject{}, fmt.Errorf("protect temporary object: %w", err)
	}
	hash := sha256.New()
	written, err := copyWithContext(ctx, io.MultiWriter(temporary, hash), io.LimitReader(source, maxBytes+1))
	if err != nil {
		return StoredObject{}, fmt.Errorf("store object: %w", err)
	}
	if written > maxBytes {
		return StoredObject{}, fmt.Errorf("%w: file exceeds configured size limit", ErrInvalidInput)
	}
	if written == 0 {
		return StoredObject{}, fmt.Errorf("%w: file is empty", ErrInvalidInput)
	}
	if err := temporary.Sync(); err != nil {
		return StoredObject{}, fmt.Errorf("flush object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return StoredObject{}, fmt.Errorf("close object: %w", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		return StoredObject{}, fmt.Errorf("generated storage key already exists")
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return StoredObject{}, fmt.Errorf("commit object: %w", err)
	}
	committed = true
	return StoredObject{Key: key, Size: written, Checksum: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *LocalObjectStorage) Open(ctx context.Context, key string) (ReadSeekCloserAt, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	path, err := s.resolve(key)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open object: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("stat object: %w", err)
	}
	return file, info.Size(), nil
}

func (s *LocalObjectStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *LocalObjectStorage) resolve(key string) (string, error) {
	key = filepath.FromSlash(strings.TrimSpace(key))
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("invalid storage key")
	}
	target := filepath.Join(s.root, filepath.Clean(key))
	relative, err := filepath.Rel(s.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage key leaves configured root")
	}
	return target, nil
}

func randomStorageKey(extension string) (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	encoded := hex.EncodeToString(value)
	return encoded[:2] + "/" + encoded[2:] + extension, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}
