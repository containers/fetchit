package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrSOPSConfig  = errors.New("invalid SOPS key configuration")
	ErrSOPSInput   = errors.New("invalid encrypted SOPS input")
	ErrSOPSSize    = errors.New("SOPS size limit exceeded")
	ErrSOPSDecrypt = errors.New("SOPS decryption failed")
)

const (
	sopsExecutable = "/usr/local/bin/sops"
	sopsFileLimit  = 8 << 20
	sopsBatchLimit = 32 << 20
)

// SOPS configures authenticated YAML decryption with a local age identity file.
type SOPS struct {
	AgeKeyFile string `mapstructure:"ageKeyFile"`
}

func (s *SOPS) validate() error {
	if s == nil || !filepath.IsAbs(s.AgeKeyFile) {
		return fmt.Errorf("%w: absolute ageKeyFile required", ErrSOPSConfig)
	}
	info, err := os.Stat(s.AgeKeyFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: readable regular private file of at most 64 KiB required", ErrSOPSConfig)
	}
	file, err := os.Open(s.AgeKeyFile)
	if err != nil {
		return ErrSOPSConfig
	}
	file.Close()
	return nil
}

// limitedSOPSOutput stops a child process from allocating unbounded plaintext.
type limitedSOPSOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (w *limitedSOPSOutput) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > sopsFileLimit {
		w.exceeded = true
		w.cancel()
		return 0, ErrSOPSSize
	}
	return w.buffer.Write(p)
}

func (s *SOPS) decrypt(ctx context.Context, input []byte) ([]byte, error) {
	return s.decryptWithCommand(ctx, input, func(childCtx context.Context) *exec.Cmd {
		return exec.CommandContext(childCtx, sopsExecutable, "decrypt", "--input-type", "yaml", "--output-type", "yaml")
	})
}

func (s *SOPS) decryptWithCommand(ctx context.Context, input []byte, createCommand func(context.Context) *exec.Cmd) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if len(input) == 0 {
		return nil, ErrSOPSInput
	}
	if len(input) > sopsFileLimit {
		return nil, ErrSOPSSize
	}
	if err := validateSOPSMetadata(input); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSOPSInput, err.Error())
	}
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output := &limitedSOPSOutput{cancel: cancel}
	defer func() { clear(output.buffer.Bytes()) }()
	command := createCommand(childCtx)
	command.Env = []string{"HOME=/nonexistent", "PATH=/nonexistent", "SOPS_AGE_KEY_FILE=" + s.AgeKeyFile}
	command.Stdin = bytes.NewReader(input)
	command.Stdout = output
	stderr := &limitedSOPSStderr{cancel: cancel}
	command.Stderr = stderr
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		// Upstream diagnostics and process errors can contain plaintext or key data.
		if output.exceeded || stderr.exceeded {
			return nil, ErrSOPSSize
		}
		if childCtx.Err() != nil {
			return nil, childCtx.Err()
		}
		return nil, ErrSOPSDecrypt
	}
	return bytes.Clone(output.buffer.Bytes()), nil
}

// Only ordinary age recipients are accepted. Other SOPS backends must not use
// ambient credentials or contact remote key services in this first release.
var ordinaryAgeRecipient = regexp.MustCompile(`^age1[023456789acdefghjklmnpqrstuvwxyz]{58}$`)

func validateSOPSMetadata(input []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	count := 0
	for {
		var document map[string]interface{}
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("invalid encrypted YAML")
		}
		{
			for _, field := range []string{"data", "stringData"} {
				if raw, exists := document[field]; exists {
					values, ok := raw.(map[string]interface{})
					if !ok {
						return errors.New("encrypted Secret values must be a mapping")
					}
					for _, value := range values {
						text, ok := value.(string)
						if !ok || !strings.HasPrefix(text, "ENC[AES256_GCM,") {
							return errors.New("SOPS methods require encrypted Secret values")
						}
					}
				}
			}
		}
		metadata, ok := document["sops"].(map[string]interface{})
		if !ok {
			return errors.New("SOPS-enabled methods require encrypted YAML with SOPS metadata")
		}
		allowed := map[string]bool{"age": true, "mac": true, "lastmodified": true, "version": true, "encrypted_regex": true, "unencrypted_regex": true, "encrypted_suffix": true, "unencrypted_suffix": true, "encrypted_comment_regex": true, "unencrypted_comment_regex": true, "mac_only_encrypted": true}
		for key, value := range metadata {
			if allowed[key] {
				continue
			}
			// SOPS versions may emit empty lists for unused recipient backends.
			if key == "kms" || key == "gcp_kms" || key == "azure_kv" || key == "hc_vault" || key == "pgp" {
				if list, ok := value.([]interface{}); ok && len(list) == 0 {
					continue
				}
				if value == nil {
					continue
				}
			}
			return errors.New("SOPS supports only local age recipients")
		}
		if value, exists := metadata["mac_only_encrypted"]; exists && value != false {
			return errors.New("SOPS integrity must cover unencrypted resource identity fields")
		}
		recipients, ok := metadata["age"].([]interface{})
		if !ok || len(recipients) == 0 {
			return errors.New("SOPS requires age recipients")
		}
		for _, entry := range recipients {
			recipient, ok := entry.(map[string]interface{})
			if !ok {
				return errors.New("invalid SOPS age recipient")
			}
			name, ok := recipient["recipient"].(string)
			if !ok || !ordinaryAgeRecipient.MatchString(name) {
				return errors.New("SOPS supports only ordinary age recipients")
			}
		}
		mac, ok := metadata["mac"].(string)
		if !ok || !strings.HasPrefix(mac, "ENC[") {
			return errors.New("SOPS integrity metadata is required")
		}
		count++
	}
	if count == 0 {
		return errors.New("empty encrypted YAML")
	}
	return nil
}

func readSOPSInput(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read encrypted manifest")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, sopsFileLimit+1))
	if err != nil || len(data) > sopsFileLimit {
		return nil, errors.New("encrypted manifest read failed or exceeds 8 MiB")
	}
	return data, nil
}

// Discard diagnostic content but bound its volume and stop a noisy child.
type limitedSOPSStderr struct {
	count    int
	exceeded bool
	cancel   context.CancelFunc
}

func (w *limitedSOPSStderr) Write(data []byte) (int, error) {
	if w.count+len(data) > 65536 {
		w.exceeded = true
		w.cancel()
		return 0, ErrSOPSSize
	}
	w.count += len(data)
	return len(data), nil
}
