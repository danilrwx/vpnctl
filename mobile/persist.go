package mobile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// persistFiles restores the original files if any write or commit fails.
// The caller validates before entering this small multi-file transaction.
func (a *App) persistFiles(names []string, write func() error) (retErr error) {
	type snapshot struct {
		path    string
		content []byte
		mode    os.FileMode
		exists  bool
	}

	files := make([]snapshot, 0, len(names))
	for _, name := range names {
		path := filepath.Join(a.dir, name)

		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			files = append(files, snapshot{path: path})
			continue
		}

		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files = append(files, snapshot{path: path, content: content, mode: info.Mode(), exists: true})
	}

	defer func() {
		if retErr == nil {
			return
		}

		for _, file := range files {
			var err error
			if file.exists {
				err = os.WriteFile(file.path, file.content, file.mode.Perm())
				if err == nil {
					err = os.Chmod(file.path, file.mode)
				}
			} else {
				err = os.Remove(file.path)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
			}

			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("restore %s: %w", file.path, err))
			}
		}
	}()

	return write()
}
