package repokeeper

import (
	"io/ioutil"
	"path/filepath"

	"github.com/ytsiuryn/go-collection"
)

type Path string

// func (rk *RepoKeeper) processAlbumEntries(
// 	dir string, fun func(string, interface{}) error, extraArgs interface{}) error {
// 	files, err := ioutil.ReadDir(dir)
// 	if err != nil {
// 		return err
// 	}
// 	var found bool
// 	for _, info := range files {
// 		if info.IsDir() {
// 			rk.processAlbumEntries(filepath.Join(dir, info.Name()), fun, extraArgs)
// 		} else {
// 			if found = collection.ContainsStr(
// 				filepath.Ext(info.Name()),
// 				rk.conf.Repo.Extensions); found {
// 				break
// 			}
// 		}
// 	}
// 	if found {
// 		return fun(dir, extraArgs)
// 	}
// 	return nil
// }

func (rk *RepoKeeper) albumEntries(dir string, entries map[Path]int64) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}
	var nsecs int64
	for _, info := range files {
		if info.IsDir() {
			if err = rk.albumEntries(filepath.Join(dir, info.Name()), entries); err != nil {
				return err
			}
		} else {
			if collection.ContainsStr(filepath.Ext(info.Name()), rk.extensions) {
				if _, ok := entries[Path(dir)]; !ok {
					entries[Path(dir)] = 0
				}
				nsecs = info.ModTime().UnixNano()
				if entries[Path(dir)] < nsecs {
					entries[Path(dir)] = nsecs
				}
			}
		}
	}
	return nil
}

// Возвращает список файлов в соответствии с маской.
// Список может быть получен рекурсивным путем.
// func fileList(dir string, fileMask *regexp.Regexp, recursive bool) ([]string, error) {
// 	var ret []string
// 	files, err := ioutil.ReadDir(dir)
// 	if err != nil {
// 		return nil, err
// 	}
// 	for _, info := range files {
// 		if recursive && info.IsDir() {
// 			ret2, err := fileList(filepath.Join(dir, info.Name()), fileMask, recursive)
// 			if err != nil {
// 				return nil, err
// 			}
// 			ret = append(ret, ret2...)
// 		} else {
// 			if fileMask.MatchString(info.Name()) {
// 				ret = append(ret, filepath.Join(dir, info.Name()))
// 			}
// 		}
// 	}
// 	return ret, nil
// }
