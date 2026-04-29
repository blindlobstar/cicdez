package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir
}

func TestResolve_EmptyRef(t *testing.T) {
	dir := t.TempDir()

	_, _, err := Resolve(dir, "")
	if !errors.Is(err, ErrEmptyRef) {
		t.Fatal("expected error for empty ref")
	}
}

func TestResolve_NoRepo(t *testing.T) {
	dir := t.TempDir()

	_, _, err := Resolve(dir, "HEAD")
	if !errors.Is(err, ErrNoRepository) {
		t.Fatal("expected error when --ref used outside a git repo")
	}
}

func TestResolve_HEAD_ExtractsToTempdir(t *testing.T) {
	dir := initRepo(t)

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer cleanup()

	if out == dir {
		t.Fatalf("expected tempdir distinct from %q", dir)
	}
	data, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatalf("expected README.md in extracted dir: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected %q, got %q", "hello", string(data))
	}
}

func TestResolve_DirtyRepo_StillExtracts(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("expected committed README.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "uncommitted.txt")); !os.IsNotExist(err) {
		t.Errorf("uncommitted file should not be in extracted dir, stat err = %v", err)
	}
}

func TestResolve_Cleanup(t *testing.T) {
	dir := initRepo(t)

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("tempdir should exist before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("tempdir should be removed after cleanup, stat err = %v", err)
	}
}

func TestResolve_FromSubdir(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "services", "web")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "compose.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("services/web/compose.yml"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("subdir", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(subdir, "HEAD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	if !strings.HasSuffix(out, filepath.Join("services", "web")) {
		t.Errorf("expected returned path to end with services/web, got %q", out)
	}
	data, err := os.ReadFile(filepath.Join(out, "compose.yml"))
	if err != nil {
		t.Fatalf("expected compose.yml at returned path: %v", err)
	}
	if string(data) != "services: {}" {
		t.Errorf("unexpected contents: %q", string(data))
	}
}

func TestResolve_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "lib", "code.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("src/lib/code.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("nested", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(out, "src", "lib", "code.go")); err != nil {
		t.Errorf("expected nested file: %v", err)
	}
}

func TestResolve_PreservesExecutableBit(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("script.sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("script", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	info, err := os.Stat(filepath.Join(out, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("expected executable bit, got mode %v", info.Mode())
	}
}

func TestResolve_PreservesSymlink(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("target content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("target.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("link.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("with symlink", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(dir, "HEAD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	info, err := os.Lstat(filepath.Join(out, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected link.txt to be a symlink, got mode %v", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(out, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "target.txt" {
		t.Errorf("expected target %q, got %q", "target.txt", target)
	}
}

func TestResolve_Tag(t *testing.T) {
	dir := initRepo(t)
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag("v1.0.0", head.Hash(), nil); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := Resolve(dir, "v1.0.0")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("expected README.md from tag: %v", err)
	}
}

func TestResolve_UnknownRef(t *testing.T) {
	dir := initRepo(t)

	_, _, err := Resolve(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
}

// commitTreeWithSubmodule writes a tree containing a .gitmodules blob and,
// if submodPath is non-empty, a gitlink entry at submodPath -> submodHash.
// Returns the commit hash.
func commitTreeWithSubmodule(t *testing.T, repo *git.Repository, gitmodules, submodPath string, submodHash plumbing.Hash) plumbing.Hash {
	t.Helper()

	gmBlob := repo.Storer.NewEncodedObject()
	gmBlob.SetType(plumbing.BlobObject)
	w, err := gmBlob.Writer()
	if err != nil {
		t.Fatalf("blob writer: %v", err)
	}
	if _, err := w.Write([]byte(gitmodules)); err != nil {
		t.Fatalf("blob write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("blob close: %v", err)
	}
	gmHash, err := repo.Storer.SetEncodedObject(gmBlob)
	if err != nil {
		t.Fatalf("store blob: %v", err)
	}

	entries := []object.TreeEntry{
		{Name: ".gitmodules", Mode: filemode.Regular, Hash: gmHash},
	}
	if submodPath != "" {
		entries = append(entries, object.TreeEntry{
			Name: submodPath,
			Mode: filemode.Submodule,
			Hash: submodHash,
		})
	}

	tree := &object.Tree{Entries: entries}
	treeObj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(treeObj); err != nil {
		t.Fatalf("tree encode: %v", err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		t.Fatalf("store tree: %v", err)
	}

	commit := &object.Commit{
		Author:    object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		Committer: object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		Message:   "with submodule",
		TreeHash:  treeHash,
	}
	commitObj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatalf("commit encode: %v", err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatalf("store commit: %v", err)
	}
	return commitHash
}

func TestResolve_Submodule_Initialized(t *testing.T) {
	mainDir := t.TempDir()
	mainRepo, err := git.PlainInit(mainDir, false)
	if err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(mainDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subRepo, err := git.PlainInit(subDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "sub.txt"), []byte("from submodule"), 0o644); err != nil {
		t.Fatal(err)
	}
	swt, _ := subRepo.Worktree()
	if _, err := swt.Add("sub.txt"); err != nil {
		t.Fatal(err)
	}
	subHead, err := swt.Commit("sub init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}

	gitmodules := "[submodule \"subname\"]\n\tpath = sub\n\turl = ./fake\n"
	commitHash := commitTreeWithSubmodule(t, mainRepo, gitmodules, "sub", subHead)

	out, cleanup, err := Resolve(mainDir, commitHash.String())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(out, "sub", "sub.txt"))
	if err != nil {
		t.Fatalf("expected submodule file: %v", err)
	}
	if string(data) != "from submodule" {
		t.Errorf("got %q, want %q", string(data), "from submodule")
	}
	if _, err := os.Stat(filepath.Join(out, ".gitmodules")); err != nil {
		t.Errorf("expected .gitmodules in extracted dir: %v", err)
	}
}

func TestResolve_Submodule_NotInitialized(t *testing.T) {
	mainDir := t.TempDir()
	mainRepo, err := git.PlainInit(mainDir, false)
	if err != nil {
		t.Fatal(err)
	}

	gitmodules := "[submodule \"subname\"]\n\tpath = sub\n\turl = ./fake\n"
	fakeHash := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")
	commitHash := commitTreeWithSubmodule(t, mainRepo, gitmodules, "sub", fakeHash)

	_, _, err = Resolve(mainDir, commitHash.String())
	if err == nil {
		t.Fatal("expected error for uninitialized submodule")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err)
	}
}

func TestResolve_Submodule_MissingTreeEntry(t *testing.T) {
	mainDir := t.TempDir()
	mainRepo, err := git.PlainInit(mainDir, false)
	if err != nil {
		t.Fatal(err)
	}

	// .gitmodules references "sub" but the tree has no gitlink at that path.
	gitmodules := "[submodule \"subname\"]\n\tpath = sub\n\turl = ./fake\n"
	commitHash := commitTreeWithSubmodule(t, mainRepo, gitmodules, "", plumbing.ZeroHash)

	_, _, err = Resolve(mainDir, commitHash.String())
	if err == nil {
		t.Fatal("expected error when .gitmodules entry has no matching tree entry")
	}
}
