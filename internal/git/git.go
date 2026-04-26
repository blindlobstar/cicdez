package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

var (
	ErrEmptyRef     = errors.New("ref must not be empty")
	ErrNoRepository = errors.New("--ref requires a git repository")
)

func Resolve(path, ref string) (string, func(), error) {
	if ref == "" {
		return "", nil, ErrEmptyRef
	}

	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return "", nil, ErrNoRepository
	}
	if err != nil {
		return "", nil, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", nil, err
	}
	rpath, err := filepath.EvalSymlinks(wt.Filesystem.Root())
	if err != nil {
		return "", nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	path, err = filepath.Rel(rpath, path)
	if err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp("", "cicdez-source-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	if err := resolveRef(repo, ref, dir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to extract ref %q: %w", ref, err)
	}
	return filepath.Join(dir, path), cleanup, nil
}

func resolveRef(repo *git.Repository, ref, path string) error {
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return err
	}

	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return err
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}

	// tree.Files() silently skips submodule entries (they have no blob),
	// so walk all entries first to fail on gitlinks.
	if err := ensureNoSubmodules(tree); err != nil {
		return err
	}

	return tree.Files().ForEach(func(f *object.File) error {
		dst := filepath.Join(path, f.Name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if f.Mode == filemode.Symlink {
			target, err := f.Contents()
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		}

		mode, err := f.Mode.ToOSFileMode()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
		if err != nil {
			return err
		}
		defer out.Close()

		r, err := f.Reader()
		if err != nil {
			return err
		}
		defer r.Close()

		_, err = io.Copy(out, r)
		return err
	})
}

func ensureNoSubmodules(tree *object.Tree) error {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Mode == filemode.Submodule {
			return fmt.Errorf("--ref builds don't support submodules: %s", name)
		}
	}
}
