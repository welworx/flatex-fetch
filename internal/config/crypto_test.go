package config

import "testing"

func TestCredentialsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("hunter2")
	creds := map[string]string{"main": "portal-pw", "second": "other-pw"}
	if err := SaveCredentials(dir, pass, creds); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCredentials(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if got["main"] != "portal-pw" || got["second"] != "other-pw" || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestCredentialsWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	if err := SaveCredentials(dir, []byte("right"), map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredentials(dir, []byte("wrong")); err == nil {
		t.Fatal("wrong passphrase decrypted successfully")
	}
}

func TestCredentialsMissingFile(t *testing.T) {
	got, err := LoadCredentials(t.TempDir(), []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty map", got)
	}
}
