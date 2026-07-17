package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// githubAPIBase is the GitHub API root; tests point this at an httptest
// server (mirrors internal/portal's baseURL field).
var githubAPIBase = "https://api.github.com"

const githubRepo = "welworx/flatex-fetch"

var upgradeHTTPClient = &http.Client{Timeout: 60 * time.Second}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// runUpgrade handles `flatex-fetch upgrade [-check] [-y]`.
func runUpgrade(args []string) int {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	check := fs.Bool("check", false, "report whether a newer version is available, without downloading")
	yes := fs.Bool("y", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var target string
	if !*check {
		t, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		t, err = filepath.EvalSymlinks(t)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		target = t
	}

	return doUpgrade(*check, *yes, target, version)
}

// doUpgrade implements the upgrade flow against an explicit target path
// rather than calling os.Executable() itself, so tests can point it at a
// scratch file standing in for the running binary.
func doUpgrade(check, yes bool, target, currentVersion string) int {
	errExit := 1
	if check {
		errExit = 2
	}

	rel, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return errExit
	}

	isDev := currentVersion == "dev"
	upgradable := isDev
	if !isDev {
		newer, err := versionNewer(currentVersion, rel.TagName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return errExit
		}
		upgradable = newer
	}
	if !upgradable {
		fmt.Printf("up to date (%s)\n", currentVersion)
		return 0
	}

	assetName := platformAssetName()
	asset, ok := findAsset(rel.Assets, assetName)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: no release asset for this platform (%s)\n", assetName)
		return errExit
	}

	if check {
		fmt.Printf("upgrade available: %s -> %s\n", currentVersion, rel.TagName)
		return 1
	}

	sums, ok := findAsset(rel.Assets, "SHA256SUMS.txt")
	if !ok {
		fmt.Fprintln(os.Stderr, "error: release is missing SHA256SUMS.txt")
		return 1
	}

	fmt.Printf("%s -> %s\n", currentVersion, rel.TagName)
	if !yes {
		ans, err := promptLine(fmt.Sprintf("Upgrade to %s? [y/N] ", rel.TagName))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if strings.ToLower(ans) != "y" {
			fmt.Println("aborted")
			return 0
		}
	}

	binBytes, err := downloadBytes(asset.BrowserDownloadURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	sumsBytes, err := downloadBytes(sums.BrowserDownloadURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := verifyChecksum(binBytes, sumsBytes, assetName); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if err := replaceBinary(binBytes, target); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("upgraded to %s. Restart flatex-fetch to use the new version.\n", rel.TagName)
	return 0
}

func fetchLatestRelease() (*ghRelease, error) {
	resp, err := upgradeHTTPClient.Get(githubAPIBase + "/repos/" + githubRepo + "/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadBytes(url string) ([]byte, error) {
	resp, err := upgradeHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func findAsset(assets []ghAsset, name string) (ghAsset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return ghAsset{}, false
}

// platformAssetName matches the naming .github/workflows/release.yml
// builds: flatex-fetch-<GOOS>-<GOARCH>, .exe suffixed on windows.
func platformAssetName() string {
	name := fmt.Sprintf("flatex-fetch-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// parseSemver parses "vMAJOR.MINOR.PATCH".
func parseSemver(s string) (major, minor, patch int, err error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid version %q", s)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

// versionNewer reports whether latest is a newer semver than current.
func versionNewer(current, latest string) (bool, error) {
	cMaj, cMin, cPatch, err := parseSemver(current)
	if err != nil {
		return false, err
	}
	lMaj, lMin, lPatch, err := parseSemver(latest)
	if err != nil {
		return false, err
	}
	if lMaj != cMaj {
		return lMaj > cMaj, nil
	}
	if lMin != cMin {
		return lMin > cMin, nil
	}
	return lPatch > cPatch, nil
}

// verifyChecksum checks data's SHA-256 against name's entry in a
// sha256sum-format sums file ("<hex>  <name>", optionally "<hex> *<name>").
func verifyChecksum(data, sums []byte, name string) error {
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hash, fname := fields[0], strings.TrimPrefix(fields[1], "*")
		if fname != name {
			continue
		}
		if hash != want {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
		return nil
	}
	return fmt.Errorf("%s not listed in SHA256SUMS.txt", name)
}

// writeTempBinary writes data to a new temp file in dir (the same
// directory as the eventual rename target, so the final rename stays on
// one filesystem and is atomic), executable on non-windows.
func writeTempBinary(dir string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, "flatex-fetch-upgrade-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			_ = os.Remove(tmpPath)
			return "", err
		}
	}
	return tmpPath, nil
}

// renameAside installs tmp over target by renaming target out of the way
// first. A plain os.Rename(tmp, target) would fail on Windows, where
// replacing an in-use exe requires deleting it and the OS blocks that;
// renaming the running exe *away* is allowed, so this is one code path for
// all platforms. On failure to install tmp, target is restored from .old.
func renameAside(tmp, target string) error {
	old := target + ".old"
	_ = os.Remove(old) // best-effort: clear a stale .old left by a previous upgrade

	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		if rerr := os.Rename(old, target); rerr != nil {
			return fmt.Errorf("install failed (%w), and rollback from %s also failed (%v)", err, old, rerr)
		}
		return err
	}
	_ = os.Remove(old) // best-effort; on Windows this fails while the old process still runs the .old file - next upgrade's cleanup above handles it
	return nil
}

func replaceBinary(data []byte, target string) error {
	tmp, err := writeTempBinary(filepath.Dir(target), data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return renameAside(tmp, target)
}
