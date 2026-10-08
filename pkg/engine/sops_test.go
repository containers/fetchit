package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/viper"
)

const testPlainKube = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: application\nspec:\n  containers:\n  - name: app\n    image: alpine\n"
const testEncryptedMetadata = "sops:\n  age:\n  - recipient: age1test\n    enc: dummy\n  mac: ENC[dummy]\n  version: 3.13.3\n"

func sopsTestSettings(t *testing.T) *SOPS {
	t.Helper()
	path := filepath.Join(t.TempDir(), "age.txt")
	if err := os.WriteFile(path, []byte("test identity"), 0600); err != nil {
		t.Fatal(err)
	}
	return &SOPS{AgeKeyFile: path}
}

func TestSOPSMetadataPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"age", testEncryptedMetadata, true},
		{"multi document", testEncryptedMetadata + "---\n" + testEncryptedMetadata, true},
		{"plaintext", testPlainKube, false},
		{"empty", "", false},
		{"malformed", "sops: [", false},
		{"missing integrity", "sops:\n  age: []", false},
		{"cloud backend", testEncryptedMetadata + "  kms: [{arn: test}]\n", false},
		{"partial integrity", testEncryptedMetadata + "  mac_only_encrypted: true\n", false},
		{"keygroups", testEncryptedMetadata + "  key_groups: []\n", false},
		{"plugin", strings.ReplaceAll(testEncryptedMetadata, "age1test", "age-plugin-test"), false},
		{"duplicate metadata", testEncryptedMetadata + testEncryptedMetadata, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSOPSMetadata([]byte(tc.input)); (err == nil) != tc.valid {
				t.Fatalf("metadata policy: %v", err)
			}
		})
	}
}

func TestSOPSConfigDecode(t *testing.T) {
	for _, extra := range []string{"", "    sops:\n      ageKeyFile: /run/secrets/age\n"} {
		v := viper.New()
		v.SetConfigType("yaml")
		err := v.ReadConfig(strings.NewReader("targetConfigs:\n- url: repository\n  kube:\n  - name: app\n" + extra))
		if err != nil {
			t.Fatal(err)
		}
		var config FetchitConfig
		if err := v.UnmarshalExact(&config); err != nil {
			t.Fatal(err)
		}
		settings := config.TargetConfigs[0].Kube[0].SOPS
		if extra == "" && settings != nil {
			t.Fatal("decryption enabled by default")
		}
		if extra != "" && (settings == nil || settings.AgeKeyFile != "/run/secrets/age") {
			t.Fatal("SOPS config was not decoded")
		}
	}
	for _, settings := range []*SOPS{nil, {}, {AgeKeyFile: "relative"}, {AgeKeyFile: "/nonexistent"}} {
		if err := settings.validate(); err == nil {
			t.Fatal("accepted invalid key config")
		}
	}
}

func TestSOPSBatchFailureClearsPreparedPlaintext(t *testing.T) {
	k := &Kube{SOPS: sopsTestSettings(t)}
	directory := t.TempDir()
	first := filepath.Join(directory, "first.yaml")
	second := filepath.Join(directory, "second.yaml")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("encrypted"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changes := map[*object.Change]string{{To: object.ChangeEntry{Name: "first"}}: first, {To: object.ChangeEntry{Name: "second"}}: second}
	var plaintext []byte
	calls := 0
	_, err := k.prepareSOPSChanges(context.Background(), changes, func(context.Context, []byte) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("secret sentinel")
		}
		plaintext = []byte(testPlainKube)
		return plaintext, nil
	})
	if err == nil || strings.Contains(err.Error(), "secret sentinel") {
		t.Fatalf("unsafe failure: %v", err)
	}
	if !bytes.Equal(plaintext, make([]byte, len(plaintext))) {
		t.Fatal("failed batch retained plaintext")
	}
}

func TestSOPSPreparationRejectsMalformedAndOversizedOutput(t *testing.T) {
	k := &Kube{SOPS: sopsTestSettings(t)}
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	os.WriteFile(path, []byte("encrypted"), 0600)
	for _, output := range []string{"data: secret sentinel\n", strings.Repeat("x", sopsFileLimit+1), testPlainKube + "---\n" + testPlainKube} {
		_, err := k.prepareSOPSChanges(context.Background(), map[*object.Change]string{nil: path}, func(context.Context, []byte) ([]byte, error) { return []byte(output), nil })
		if err == nil || strings.Contains(err.Error(), "secret sentinel") {
			t.Fatalf("invalid plaintext accepted or leaked: %v", err)
		}
	}
}

func TestSOPSRealAgeMultiDocument(t *testing.T) {
	binary := os.Getenv("SOPS_TEST_BINARY")
	age := os.Getenv("AGE_TEST_BINARY")
	if binary == "" || age == "" {
		t.Skip("set SOPS_TEST_BINARY and AGE_TEST_BINARY for real SOPS coverage")
	}
	directory := t.TempDir()
	key := filepath.Join(directory, "age.txt")
	if err := exec.Command(age, "-o", key).Run(); err != nil {
		t.Fatal("age key generation failed")
	}
	recipient, err := exec.Command(age, "-y", key).Output()
	if err != nil {
		t.Fatal("age public key failed")
	}
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: application-secret\nstringData:\n  password: secret-sentinel\n---\n" + testPlainKube
	command := exec.Command(binary, "encrypt", "--filename-override", "manifest.yaml", "--age", strings.TrimSpace(string(recipient)), "--encrypted-regex", "^(data|stringData)$", "--input-type", "yaml", "--output-type", "yaml")
	command.Stdin = strings.NewReader(secret)
	ciphertext, err := command.Output()
	if err != nil {
		t.Fatal("SOPS encryption failed")
	}
	if bytes.Contains(ciphertext, []byte("secret-sentinel")) {
		t.Fatal("secret not encrypted")
	}
	settings := &SOPS{AgeKeyFile: key}
	plain, err := settings.decryptWithExecutable(context.Background(), ciphertext, binary)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	if !bytes.Contains(plain, []byte("secret-sentinel")) || validateDecryptedKube(plain) != nil {
		t.Fatal("multi-document decryption failed")
	}
	// Change authenticated visible metadata; MAC verification must reject it.
	tampered := bytes.Replace(ciphertext, []byte("application-secret"), []byte("tampered-secret"), 1)
	if _, err := settings.decryptWithExecutable(context.Background(), tampered, binary); err == nil {
		t.Fatal("tampered input decrypted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := settings.decryptWithExecutable(cancelled, ciphertext, binary); err == nil {
		t.Fatal("cancelled decryption succeeded")
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
	wrongKey := filepath.Join(directory, "wrong.txt")
	if err := exec.Command(age, "-o", wrongKey).Run(); err != nil {
		t.Fatal("age key generation failed")
	}
	if _, err := (&SOPS{AgeKeyFile: wrongKey}).decryptWithExecutable(context.Background(), ciphertext, binary); err == nil {
		t.Fatal("wrong key decrypted")
	}
}

func TestSOPSExecutableFailuresDoNotExposeOutput(t *testing.T) {
	settings := sopsTestSettings(t)
	binary := filepath.Join(t.TempDir(), "sops")
	script := "#!/bin/sh\necho secret-sentinel >&2\necho secret-sentinel\nexit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	plain, err := settings.decryptWithExecutable(context.Background(), []byte(testEncryptedMetadata), binary)
	if err == nil || len(plain) != 0 || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("unsafe executable error: %v", err)
	}
}

func TestSOPSOutputLimitCancelsExecution(t *testing.T) {
	settings := sopsTestSettings(t)
	binary := filepath.Join(t.TempDir(), "sops")
	// A portable pipeline generates a result just above the limit without keys.
	script := "#!/bin/sh\nhead -c 8388609 /dev/zero\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if plain, err := settings.decryptWithExecutable(context.Background(), []byte(testEncryptedMetadata), binary); err == nil || len(plain) != 0 {
		t.Fatal("accepted oversized plaintext")
	}
}
