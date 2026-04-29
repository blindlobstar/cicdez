package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
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
	if err := resolveWithSubmodules(rpath, ref, dir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to extract ref %q: %w", ref, err)
	}
	return filepath.Join(dir, path), cleanup, nil
}

func resolveWithSubmodules(path, ref, dest string) error {
	if ref == "" {
		return ErrEmptyRef
	}

	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: false,
	})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return ErrNoRepository
	}
	if err != nil {
		return err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	rpath, err := filepath.EvalSymlinks(wt.Filesystem.Root())
	if err != nil {
		return err
	}

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

	gmt, err := tree.File(".gitmodules")
	if err != nil && !errors.Is(err, object.ErrFileNotFound) {
		return err
	}

	if gmt != nil {
		modulecontent, err := gmt.Contents()
		if err != nil {
			return err
		}
		m := config.NewModules()
		if err := m.Unmarshal([]byte(modulecontent)); err != nil {
			return err
		}

		for _, sm := range m.Submodules {
			se, err := tree.FindEntry(sm.Path)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Join(dest, sm.Path), 0o755); err != nil {
				return err
			}

			err = resolveWithSubmodules(filepath.Join(rpath, sm.Path), se.Hash.String(), filepath.Join(dest, sm.Path))
			if errors.Is(err, ErrNoRepository) {
				return fmt.Errorf("submodule %q not initialized — run `git submodule update --init --recursive`", sm.Path)
			}
			if err != nil {
				return err
			}

		}
	}

	return tree.Files().ForEach(func(f *object.File) error {
		dst := filepath.Join(dest, f.Name)
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
