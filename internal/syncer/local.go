package syncer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
)

// errEmptySource stops a sync before it can delete the mirror: an unmounted
// folder looks exactly like one whose files were all removed.
var errEmptySource = errors.New("local vault is empty or not mounted")

// localBucket reads a folder the author syncs by other means (Syncthing, git, Obsidian Sync).
// It only reads: the syncer still copies every file through the size limits into the mirror.
type localBucket struct {
	dir string
}

// NewLocal returns a Bucket over dir. The folder is opened on each call, so it may appear later.
func NewLocal(dir string) Bucket { return &localBucket{dir: dir} }

// localETag changes when a file is rewritten; hashing every file on every poll would cost too much.
func localETag(fi fs.FileInfo) string {
	return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().UnixNano())
}

func (b *localBucket) List(ctx context.Context, max int) ([]Object, bool, error) {
	root, err := os.OpenRoot(b.dir)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = root.Close() }()

	var out []Object
	err = fs.WalkDir(root.FS(), ".", func(key string, d fs.DirEntry, err error) error {
		if err != nil {
			if key != "." && errors.Is(err, fs.ErrNotExist) {
				return nil // deleted during the walk
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if key == "." {
			return nil
		}
		// Pruned here, not left to mirror.Classify: MAX_OBJECTS counts every listed key, and .git alone can fill it.
		if name := d.Name(); name[0] == '.' || name[0] == '_' {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() { // folders are walked; symlinks are never followed
			return nil
		}
		fi, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		out = append(out, Object{Key: key, ETag: localETag(fi), Size: fi.Size()})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if len(out) == 0 {
		return nil, false, errEmptySource
	}
	// WalkDir sorts per folder, which is not key order: "a-b" sorts before "a/b".
	slices.SortFunc(out, func(a, b Object) int { return cmp.Compare(a.Key, b.Key) })
	if len(out) > max {
		return out[:max], true, nil
	}
	return out, false, nil
}

func (b *localBucket) Get(_ context.Context, key, etag string) (io.ReadCloser, error) {
	root, err := os.OpenRoot(b.dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }() // the opened file stays valid

	// Lstat first: Open would follow a symlink swapped in after the listing.
	before, err := root.Lstat(key)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := root.Open(key)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err == nil && (!fi.Mode().IsRegular() || localETag(fi) != etag) {
		err = errors.New("file changed since the listing")
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
