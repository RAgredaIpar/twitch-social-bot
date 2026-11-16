package updater_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/updater"
)

func TestCheck_ReleaseUpdateDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		asset := "twitch-social-bot-linux.tar.gz"
		if runtime.GOOS == "windows" {
			asset = "twitch-social-bot-windows.zip"
		}
		_, _ = w.Write([]byte(`{
			"tag_name": "v2.0.0",
			"assets": [
				{"name": "` + asset + `", "browser_download_url": "https://example.com/` + asset + `"}
			]
		}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	info, err := updater.Check(context.Background(), updater.Config{
		Repo:       "owner/repo",
		Version:    "v1.0.0",
		WorkDir:    dir,
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected update info, got nil")
	}
	if info.Mode != updater.ModeRelease || info.CurrentVersion != "v1.0.0" || info.LatestVersion != "v2.0.0" {
		t.Fatalf("unexpected update info: %+v", info)
	}
}

func TestCheck_SameVersionReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v1.0.0", "assets": []}`))
	}))
	defer srv.Close()

	info, err := updater.Check(context.Background(), updater.Config{
		Repo:       "owner/repo",
		Version:    "v1.0.0",
		WorkDir:    t.TempDir(),
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil update info when up to date, got %+v", info)
	}
}

func TestCheckAndApply_GitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work1 := filepath.Join(root, "work1")
	work2 := filepath.Join(root, "work2")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run(root, "init", "--bare", remote)
	run(root, "clone", remote, work1)
	run(work1, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(work1, "version.txt"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(work1, "add", ".")
	run(work1, "commit", "-m", "v1")
	run(work1, "push", "-u", "origin", "main")

	run(root, "clone", "-b", "main", remote, work2)
	// Preserve an untracked .env in work2
	if err := os.WriteFile(filepath.Join(work2, ".env"), []byte("LLM_API_KEY=keep-me\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Push v2 from work1
	if err := os.WriteFile(filepath.Join(work1, "version.txt"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(work1, "add", ".")
	run(work1, "commit", "-m", "v2")
	run(work1, "push", "origin", "main")

	cfg := updater.Config{
		Branch:  "main",
		WorkDir: work2,
	}
	info, err := updater.Check(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected check error: %v", err)
	}
	if info == nil || info.Mode != updater.ModeGit {
		t.Fatalf("expected ModeGit update info, got %+v", info)
	}

	if err := updater.Apply(context.Background(), cfg, info); err != nil {
		t.Fatalf("unexpected apply error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(work2, "version.txt"))
	if err != nil || strings.TrimSpace(string(got)) != "v2" {
		t.Fatalf("expected version.txt updated to v2, got %q (%v)", string(got), err)
	}
	envGot, err := os.ReadFile(filepath.Join(work2, ".env"))
	if err != nil || string(envGot) != "LLM_API_KEY=keep-me\n" {
		t.Fatalf("expected .env preserved, got %q (%v)", string(envGot), err)
	}
}
