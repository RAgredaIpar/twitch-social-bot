package updater

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultRepo   = "RAgredaIpar/twitch-social-bot"
	DefaultBranch = "main"
)

// Mode identifies how the local installation can be updated.
type Mode string

const (
	ModeGit     Mode = "git"
	ModeRelease Mode = "release"
)

// UpdateInfo describes an available update.
type UpdateInfo struct {
	Mode           Mode
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	AssetName      string
}

// Config configures update detection and application.
type Config struct {
	Repo       string
	Branch     string
	Version    string // Semantic tag like "v1.1.0" or "dev"
	WorkDir    string
	HTTPClient *http.Client
	APIBaseURL string // Overridable for tests; defaults to https://api.github.com
}

func (c *Config) defaults() {
	if c.Repo == "" {
		c.Repo = DefaultRepo
	}
	if c.Branch == "" {
		c.Branch = DefaultBranch
	}
	if c.WorkDir == "" {
		c.WorkDir = "."
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 8 * time.Second}
	}
	if c.APIBaseURL == "" {
		c.APIBaseURL = "https://api.github.com"
	}
}

const (
	cleanupRetryAttempts = 30
	cleanupRetryInterval = 500 * time.Millisecond
)

// CleanupOldBinary removes any leftover .old executables from prior Windows self-updates.
// If the prior updater process is still exiting, removal retries in the background.
func CleanupOldBinary() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	CleanupOldBinariesForExe(exe)
}

// CleanupOldBinariesForExe removes exe+".old" and any stale "<exeBase>*.old*" siblings
// in the same directory so repeated self-updates never leave orphaned .old files behind.
func CleanupOldBinariesForExe(exe string) {
	if strings.TrimSpace(exe) == "" {
		return
	}
	seen := map[string]bool{}
	schedule := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		seen[key] = true
		CleanupPathWithRetry(p, cleanupRetryAttempts, cleanupRetryInterval)
	}

	schedule(exe + ".old")

	dir := filepath.Dir(exe)
	baseLower := strings.ToLower(filepath.Base(exe))
	// Strip any trailing .old suffix in case the user launched from an old backup name.
	for strings.HasSuffix(baseLower, ".old") {
		baseLower = strings.TrimSuffix(baseLower, ".old")
	}
	if baseLower == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		nameLower := strings.ToLower(entry.Name())
		if nameLower == strings.ToLower(filepath.Base(exe)) {
			continue
		}
		if strings.HasPrefix(nameLower, baseLower) && strings.Contains(nameLower, ".old") {
			schedule(filepath.Join(dir, entry.Name()))
		}
	}
}

// CleanupPathWithRetry removes targetPath if present, retrying briefly in the background
// when the OS still holds a transient lock (such as a parent Windows .exe.old exiting).
func CleanupPathWithRetry(targetPath string, attempts int, delay time.Duration) {
	err := os.Remove(targetPath)
	if err == nil || os.IsNotExist(err) {
		return
	}
	if attempts <= 1 {
		return
	}
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	go func() {
		for i := 1; i < attempts; i++ {
			time.Sleep(delay)
			if err := os.Remove(targetPath); err == nil || os.IsNotExist(err) {
				return
			}
		}
	}()
}

// Check inspects either the local git repository or GitHub Releases for a newer version.
// Network errors fail open (returns nil, nil) so offline users are not locked out.
func Check(ctx context.Context, cfg Config) (*UpdateInfo, error) {
	cfg.defaults()

	gitDir := filepath.Join(cfg.WorkDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return checkGit(ctx, cfg)
	}

	if cfg.Version != "" && cfg.Version != "dev" {
		return checkRelease(ctx, cfg)
	}
	return nil, nil
}

func checkGit(ctx context.Context, cfg Config) (*UpdateInfo, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	localOut, err := runGit(checkCtx, cfg.WorkDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil
	}
	localSHA := strings.TrimSpace(localOut)

	remoteOut, err := runGit(checkCtx, cfg.WorkDir, "ls-remote", "origin", "refs/heads/"+cfg.Branch)
	if err != nil {
		return nil, nil
	}
	fields := strings.Fields(remoteOut)
	if len(fields) == 0 {
		return nil, nil
	}
	remoteSHA := strings.TrimSpace(fields[0])

	if localSHA == "" || remoteSHA == "" || localSHA == remoteSHA {
		return nil, nil
	}

	return &UpdateInfo{
		Mode:           ModeGit,
		CurrentVersion: shortSHA(localSHA),
		LatestVersion:  shortSHA(remoteSHA),
	}, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func checkRelease(ctx context.Context, cfg Config) (*UpdateInfo, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(cfg.APIBaseURL, "/"), cfg.Repo)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "twitch-social-bot-updater")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil
	}
	defer resp.Body.Close()

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, nil
	}
	latest := strings.TrimSpace(rel.TagName)
	if latest == "" || latest == cfg.Version {
		return nil, nil
	}

	var dlURL, assetName string
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if runtime.GOOS == "windows" {
			if strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".exe") {
				dlURL = a.BrowserDownloadURL
				assetName = a.Name
				if strings.HasSuffix(lower, ".zip") {
					break
				}
			}
		} else if strings.Contains(lower, runtime.GOOS) {
			dlURL = a.BrowserDownloadURL
			assetName = a.Name
			break
		}
	}
	if dlURL == "" {
		return nil, nil
	}

	return &UpdateInfo{
		Mode:           ModeRelease,
		CurrentVersion: cfg.Version,
		LatestVersion:  latest,
		DownloadURL:    dlURL,
		AssetName:      assetName,
	}, nil
}

// Apply downloads and installs the update described by info.
// Note: .env, .tokens.json, and data/ are preserved.
func Apply(ctx context.Context, cfg Config, info *UpdateInfo) error {
	cfg.defaults()
	if info == nil {
		return errors.New("nil update info")
	}

	switch info.Mode {
	case ModeGit:
		return applyGit(ctx, cfg)
	case ModeRelease:
		return applyRelease(ctx, cfg, info)
	default:
		return fmt.Errorf("unsupported update mode: %s", info.Mode)
	}
}

func applyGit(ctx context.Context, cfg Config) error {
	if _, err := runGit(ctx, cfg.WorkDir, "fetch", "origin", cfg.Branch); err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	if _, err := runGit(ctx, cfg.WorkDir, "reset", "--hard", "origin/"+cfg.Branch); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}

	// If running from a compiled binary inside the git repo (not `go run`), rebuild it.
	exe, err := os.Executable()
	if err == nil && !isGoRunExecutable(exe) {
		if _, lookErr := exec.LookPath("go"); lookErr == nil {
			newExe := exe + ".new"
			buildCmd := exec.CommandContext(ctx, "go", "build", "-o", newExe, "./cmd/bot")
			buildCmd.Dir = cfg.WorkDir
			if out, bErr := buildCmd.CombinedOutput(); bErr != nil {
				_ = os.Remove(newExe)
				return fmt.Errorf("go build after update: %w (%s)", bErr, strings.TrimSpace(string(out)))
			}
			if rErr := replaceExecutable(exe, newExe); rErr != nil {
				return rErr
			}
		}
	}
	return nil
}

func applyRelease(ctx context.Context, cfg Config, info *UpdateInfo) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "twitch-social-bot-updater")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release returned HTTP %d", resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "tsb-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	assetPath := filepath.Join(tmpDir, info.AssetName)
	f, err := os.Create(assetPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	if strings.HasSuffix(strings.ToLower(info.AssetName), ".zip") {
		return installFromZip(assetPath, exe, cfg.WorkDir)
	}
	return replaceExecutable(exe, assetPath)
}

func installFromZip(zipPath, currentExe, workDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open update zip: %w", err)
	}
	defer zr.Close()

	exeBase := strings.ToLower(filepath.Base(currentExe))
	var newExePath string

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		clean := filepath.Clean(zf.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}

		// Preserve user config/tokens/data if ever included.
		baseLower := strings.ToLower(filepath.Base(clean))
		if baseLower == ".env" || baseLower == ".tokens.json" {
			continue
		}

		if strings.HasSuffix(baseLower, ".exe") && (baseLower == exeBase || baseLower == "twitch-social-bot.exe" || newExePath == "") {
			tmpExe := currentExe + ".new"
			if err := extractZipEntry(zf, tmpExe, 0755); err != nil {
				return err
			}
			newExePath = tmpExe
			continue
		}

		// Update personas/, locales/, and SOUL.*.md relative to workDir.
		if strings.HasPrefix(clean, "personas"+string(filepath.Separator)) ||
			strings.HasPrefix(clean, "locales"+string(filepath.Separator)) ||
			strings.HasPrefix(strings.ToUpper(clean), "SOUL.") {
			dest := filepath.Join(workDir, clean)
			if err := extractZipEntry(zf, dest, 0644); err != nil {
				return err
			}
		}
	}

	if newExePath != "" {
		return replaceExecutable(currentExe, newExePath)
	}
	return nil
}

func extractZipEntry(zf *zip.File, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func replaceExecutable(currentExe, newExe string) error {
	oldExe := currentExe + ".old"
	if err := os.Remove(oldExe); err != nil && !os.IsNotExist(err) {
		// If a previous .old file is still locked, move it aside to a unique .old name
		// so the current update can proceed and CleanupOldBinariesForExe sweeps both on restart.
		fallbackOld := fmt.Sprintf("%s.%d.old", currentExe, time.Now().UnixNano())
		_ = os.Rename(oldExe, fallbackOld)
	}
	if err := os.Rename(currentExe, oldExe); err != nil {
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err := moveFile(newExe, currentExe); err != nil {
		_ = os.Rename(oldExe, currentExe)
		return fmt.Errorf("install new executable: %w", err)
	}
	return nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}

// Relaunch starts a fresh process of the updated bot. On Windows it opens the updated
// binary in a detached console so the replaced .exe.old exits and can be cleaned up automatically.
func Relaunch(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if isGoRunExecutable(exe) {
		cmdArgs := append([]string{"run", "./cmd/bot"}, args...)
		cmd := exec.Command("go", cmdArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	return relaunchBinary(exe, args)
}

func isGoRunExecutable(exe string) bool {
	lower := strings.ToLower(exe)
	return strings.Contains(lower, "go-build")
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
