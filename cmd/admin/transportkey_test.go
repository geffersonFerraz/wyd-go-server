package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbsrv/internal/protocol"
)

func TestTransportKeyIsCreatedOnceInTheEnvFile(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	pub := filepath.Join(dir, "serverkey.txt")
	if err := os.WriteFile(env, []byte("POSTGRES_PASSWORD=x"), 0o600); err != nil { // no final newline
		t.Fatal(err)
	}
	if err := transportKey([]string{"-env", env, "-pub", pub}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(env)
	lines := strings.Split(strings.TrimSpace(string(first)), "\n")
	if len(lines) != 2 || lines[0] != "POSTGRES_PASSWORD=x" || !strings.HasPrefix(lines[1], transportKeyVar+"=") {
		t.Fatalf("env file: %q", first)
	}
	key, err := protocol.ParseTransportKey(strings.TrimPrefix(lines[1], transportKeyVar+"="))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(pub)
	got, err := protocol.ParsePublicKey(string(text))
	if err != nil || !got.Equal(key.PublicKey()) {
		t.Fatalf("serverkey.txt %q does not match the env key: %v", text, err)
	}

	// Again: same key, file untouched, public file rewritten.
	_ = os.Remove(pub)
	if err := transportKey([]string{"-env", env, "-pub", pub}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(env)
	again, _ := os.ReadFile(pub)
	if string(second) != string(first) || string(again) != string(text) {
		t.Fatal("the second run changed the key")
	}
}

func TestTransportKeyRefusesABrokenEnvFile(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte(transportKeyVar+"=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := transportKey([]string{"-env", env}); err == nil {
		t.Fatal("accepted a malformed key")
	}
}
