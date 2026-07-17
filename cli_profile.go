package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/welworx/flatex-fetch/internal/config"
)

// readPassphrase returns the credentials.enc master passphrase:
// FLATEX_FETCH_PASSPHRASE if set (cron/scripting), else an interactive
// prompt. confirmNew prompts twice — use when the file doesn't exist yet.
func readPassphrase(confirmNew bool) ([]byte, error) {
	if p := os.Getenv("FLATEX_FETCH_PASSPHRASE"); p != "" {
		return []byte(p), nil
	}
	p1, err := promptSecret("Passphrase: ")
	if err != nil {
		return nil, err
	}
	if confirmNew {
		p2, err := promptSecret("Repeat passphrase: ")
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(p1, p2) {
			return nil, errors.New("passphrases do not match")
		}
	}
	return p1, nil
}

func promptSecret(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	p, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return p, err
}

func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}

func profileAdd(dir, name, domain, username, password string) error {
	ps, err := config.LoadProfiles(dir)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Name == name {
			return fmt.Errorf("profile %q already exists", name)
		}
	}
	pass, err := readPassphrase(!config.CredentialsExist(dir))
	if err != nil {
		return err
	}
	creds, err := config.LoadCredentials(dir, pass)
	if err != nil {
		return err
	}
	creds[name] = password
	if err := config.SaveCredentials(dir, pass, creds); err != nil {
		return err
	}
	return config.SaveProfiles(dir, append(ps, config.Profile{Name: name, Username: username, Domain: domain}))
}

func profileRemove(dir, name string) error {
	ps, err := config.LoadProfiles(dir)
	if err != nil {
		return err
	}
	kept := ps[:0]
	found := false
	for _, p := range ps {
		if p.Name == name {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("no profile %q", name)
	}
	if config.CredentialsExist(dir) {
		pass, err := readPassphrase(false)
		if err != nil {
			return err
		}
		creds, err := config.LoadCredentials(dir, pass)
		if err != nil {
			return err
		}
		delete(creds, name)
		if err := config.SaveCredentials(dir, pass, creds); err != nil {
			return err
		}
	}
	return config.SaveProfiles(dir, kept)
}

// runProfile handles `flatex-fetch profile <add|list|remove> ...`.
func runProfile(args []string) int {
	if len(args) == 0 {
		return usage()
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return usage()
		}
		name := args[1]
		domain := "flatex.at"
		if len(args) >= 4 && args[2] == "-domain" {
			domain = args[3]
		}
		username, err := promptLine("Username: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		pw, err := promptSecret("Portal password: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if err := profileAdd(dir, name, domain, username, string(pw)); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("profile", name, "added")
		return 0
	case "list":
		ps, err := config.LoadProfiles(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		for _, p := range ps {
			fmt.Printf("%s\t%s\t%s\n", p.Name, p.Username, p.Domain)
		}
		return 0
	case "remove":
		if len(args) < 2 {
			return usage()
		}
		if err := profileRemove(dir, args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("profile", args[1], "removed")
		return 0
	default:
		return usage()
	}
}
