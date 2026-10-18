package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// apiUID — пользователь nonroot в distroless, от него работает rendol-api.
const apiUID = 65532

// prepareDirs запускается init-контейнером от root и отдаёт rendol-api его
// каталоги на узле. На узлах, поставленных до перехода на nonroot, там лежат
// файлы root, которые api иначе не прочтёт.
//
// В host/ стоит setgid: файлы, которые пишет rendol-updater от root
// (status.json, 0640), получают группу api и остаются ей доступны.
func prepareDirs(dirs map[string]os.FileMode) error {
	for dir, mode := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := os.Lchown(p, apiUID, apiUID); err != nil {
				return err
			}
			if d.IsDir() && mode&os.ModeSetgid != 0 {
				fi, err := d.Info()
				if err != nil {
					return err
				}
				return os.Chmod(p, fi.Mode().Perm()|os.ModeSetgid)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			return err
		}
	}
	return nil
}
