package engine

import (
	"bytes"
	"context"
	"errors"
	"github.com/go-git/go-git/v5"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/viper"
)

const testPlainKube = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: application\nspec:\n  containers:\n  - name: app\n    image: alpine\n"

var testEncryptedMetadata = "sops:\n  age:\n  - recipient: age1" + strings.Repeat("q", 58) + "\n    enc: dummy\n  mac: ENC[dummy]\n  version: 3.13.3\n"

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
		{"plugin", strings.ReplaceAll(testEncryptedMetadata, "age1"+strings.Repeat("q", 58), "age1plugin1test"), false},
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
	plain, err := decryptSOPSTest(settings, context.Background(), ciphertext, binary)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	if !bytes.Contains(plain, []byte("secret-sentinel")) || validateDecryptedKube(plain) != nil {
		t.Fatal("multi-document decryption failed")
	}
	// Change authenticated visible metadata; MAC verification must reject it.
	tampered := bytes.Replace(ciphertext, []byte("application-secret"), []byte("tampered-secret"), 1)
	if _, err := decryptSOPSTest(settings, context.Background(), tampered, binary); err == nil {
		t.Fatal("tampered input decrypted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decryptSOPSTest(settings, cancelled, ciphertext, binary); err == nil {
		t.Fatal("cancelled decryption succeeded")
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
	wrongKey := filepath.Join(directory, "wrong.txt")
	if err := exec.Command(age, "-o", wrongKey).Run(); err != nil {
		t.Fatal("age key generation failed")
	}
	if _, err := decryptSOPSTest(&SOPS{AgeKeyFile: wrongKey}, context.Background(), ciphertext, binary); err == nil {
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
	plain, err := decryptSOPSTest(settings, context.Background(), []byte(testEncryptedMetadata), binary)
	if err == nil || len(plain) != 0 || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("unsafe executable error: %v", err)
	}
}

func TestSOPSOutputLimitCancelsExecution(t *testing.T) {
	settings := sopsTestSettings(t)
	binary := filepath.Join(t.TempDir(), "sops")
	// A portable pipeline generates a result just above the limit without keys.
	script := "#!/bin/sh\n/usr/bin/head -c 8388609 /dev/zero\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if plain, err := decryptSOPSTest(settings, context.Background(), []byte(testEncryptedMetadata), binary); err == nil || len(plain) != 0 {
		t.Fatal("accepted oversized plaintext")
	}
}

func TestSOPSPreparationUsesGitVersionsForUpdateAndDeletion(t *testing.T) {
	settings := sopsTestSettings(t)
	t.Chdir(t.TempDir())
	repo := mirrorSource(t, "repo")
	if err := os.MkdirAll("repo/kube", 0755); err != nil {
		t.Fatal(err)
	}
	tree, _ := repo.Worktree()
	commit := func(value string) plumbing.Hash {
		if err := os.WriteFile("repo/kube/manifest.yaml", []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := tree.Add("kube"); err != nil {
			t.Fatal(err)
		}
		hash, err := tree.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	first := commit("old ciphertext")
	second := commit("new ciphertext")
	k := &Kube{CommonMethod: CommonMethod{target: &Target{url: "https://example.invalid/repo.git"}, TargetPath: "kube"}, SOPS: settings}
	changes, err := applyChanges(context.Background(), k.target, k.TargetPath, nil, first, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("repo/kube/manifest.yaml", []byte("uncommitted ciphertext"), 0600)
	var inputs []string
	decode := func(_ context.Context, input []byte) ([]byte, error) {
		inputs = append(inputs, string(input))
		return []byte(testPlainKube), nil
	}
	prepared, err := k.prepareSOPSChanges(context.Background(), changes, decode)
	if err != nil {
		t.Fatal(err)
	}
	clearPreparedKube(prepared)
	if len(inputs) != 2 || inputs[0] != "old ciphertext" || inputs[1] != "new ciphertext" {
		t.Fatalf("read wrong Git versions: %v", inputs)
	}
	for change := range changes {
		changes[change] = deleteFile
	}
	inputs = nil
	prepared, err = k.prepareSOPSChanges(context.Background(), changes, decode)
	if err != nil {
		t.Fatal(err)
	}
	defer clearPreparedKube(prepared)
	if len(inputs) != 1 || inputs[0] != "old ciphertext" || prepared[0].next != nil {
		t.Fatal("deletion did not use old Git content")
	}
}

func TestSOPSEmptyBlockIsInvalid(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("targetConfigs:\n- url: repository\n  kube:\n  - name: app\n    sops: {}\n")); err != nil {
		t.Fatal(err)
	}
	var config FetchitConfig
	if err := v.UnmarshalExact(&config); err != nil {
		t.Fatal(err)
	}
	settings := config.TargetConfigs[0].Kube[0].SOPS
	if settings == nil || settings.validate() == nil {
		t.Fatal("empty SOPS block silently disabled decryption")
	}
}

// Alternate executables are confined to test code; production always uses the
// fixed checksum-verified /usr/local/bin/sops binary.
func decryptSOPSTest(settings *SOPS, ctx context.Context, input []byte, binary string) ([]byte, error) {
	return settings.decryptWithCommand(ctx, input, func(child context.Context) *exec.Cmd {
		return exec.CommandContext(child, binary, "decrypt", "--input-type", "yaml", "--output-type", "yaml")
	})
}

func TestSOPSRejectsUnencryptedSecretAndMixedInput(t *testing.T) {
	for _, input := range []string{
		"apiVersion: v1\nkind: Secret\ndata:\n  password: exposed\n" + testEncryptedMetadata,
		testEncryptedMetadata + "---\n" + testPlainKube,
		strings.Replace(testEncryptedMetadata, "mac: ENC[dummy]", "mac: invalid", 1),
	} {
		if err := validateSOPSMetadata([]byte(input)); err == nil {
			t.Fatal("accepted invalid encrypted input")
		}
	}
}

func TestSOPSStderrVolumeIsBounded(t *testing.T) {
	settings := sopsTestSettings(t)
	binary := filepath.Join(t.TempDir(), "sops")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n/usr/bin/head -c 65537 /dev/zero >&2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := decryptSOPSTest(settings, context.Background(), []byte(testEncryptedMetadata), binary)
	if !errors.Is(err, ErrSOPSSize) {
		t.Fatalf("lost diagnostic size error: %v", err)
	}
}

func TestSOPSDeletionAndNoChangesSkipNetworkPreflight(t *testing.T) {
	k := &Kube{Networks: []string{"missing-network"}}
	for _, changes := range [][]preparedKubeChange{nil, {{path: deleteFile}}} {
		if err := k.runPreparedSOPS(context.Background(), context.Background(), changes); err != nil {
			t.Fatalf("cleanup required unavailable network: %v", err)
		}
	}
}

func TestSOPSInvalidInputStopsBeforePodman(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(testPlainKube), 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kube{SOPS: sopsTestSettings(t)}
	// An absent connection would make stop/play fail. Invalid input must instead
	// return the preparation classification before any mutation is attempted.
	err := k.MethodEngine(context.Background(), context.Background(), nil, path)
	if !errors.Is(err, ErrSOPSInput) {
		t.Fatalf("did not stop at preparation: %v", err)
	}
}
