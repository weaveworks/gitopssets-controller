package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ignoreExists(err error) error {
	if errors.Is(err, fs.ErrExist) {
		return nil
	}

	return err
}

func copyFile(dst, src string) error {
	// TODO: this can use io.Copy
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	buf, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, buf, st.Mode())
}

func copyTree(dst, src string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		// re-stat the path so that we can tell whether it is a symlink
		info, err = os.Lstat(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targ := filepath.Join(dst, rel)

		switch {
		case info.IsDir():
			return ignoreExists(os.Mkdir(targ, 0755))
		case info.Mode()&os.ModeSymlink != 0:
			referent, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(referent, targ)
		default:
			return copyFile(targ, path)
		}
	})
}

type localFetcher struct {
	logger logr.Logger
}

func (l localFetcher) FetchWithContext(_ context.Context, archiveURL, checksum, dir string) error {
	parsed, err := url.Parse(archiveURL)
	if err != nil {
		return err
	}
	l.logger.Info("setting up archive", "archiveURL", archiveURL)

	return copyTree(dir, parsed.Path)
}

type localObjectReader struct {
	repositoryRoot string
	logger         logr.Logger
}

func (l localObjectReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	base, err := filepath.Abs(l.repositoryRoot)
	if err != nil {
		return err
	}

	l.logger.Info("reading from local filesystem", "base", base)

	switch v := obj.(type) {
	case *sourcev1.GitRepository:
		v.Status.Artifact = &meta.Artifact{
			URL: "file://" + filepath.Join(base, key.Name),
		}
	case *sourcev1.OCIRepository:
		v.Status.Artifact = &meta.Artifact{
			URL: "file://" + filepath.Join(base, key.Name),
		}

	default:
		return fmt.Errorf("filesystem access for %T not implemented", obj)
	}

	return nil
}

func (l localObjectReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("not implemented")
}
