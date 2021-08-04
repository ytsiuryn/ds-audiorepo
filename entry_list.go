package repokeeper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ytsiuryn/go-collection"
)

// FsChange - тип для описания изменений аудио каталогов
type FsChange uint8

// Допустимые изменения аудио каталогов
const (
	CreatedFsChange FsChange = iota + 1
	RenamedFsChange
	DeletedFsChange
)

// EntryCacheInfo описывает каталоговый узел для Album Entry и его родительских каталогов
// в кеше сервиса.
type EntryCacheInfo struct {
	Inode     uint64   `json:"inode"`
	ChildDirs []string `json:"children,omitempty"`
}

// Entries хранит состояние объекта формирования кеша аудио каталогов.
type Entries struct {
	RootDir    string                     `json:"root_dir"`
	Cache      map[string]*EntryCacheInfo `json:"cache"`
	Extensions []string                   `json:"extensions"`
	m          map[string]fs.FileInfo     `json:"-"`
	rootLen    int                        `json:"-"`
}

// EntryModification описывает изменение конкретного каталога.
type EntryModification struct {
	Change  FsChange
	NewName string
}

// NewEntries создает объект для формирования кеша аудио каталогов.
func NewEntries(rootDir string, extensions []string) *Entries {
	info, _ := os.Stat(rootDir)
	m := make(map[string]fs.FileInfo)
	m[rootDir] = info
	return &Entries{
		RootDir:    rootDir,
		Cache:      make(map[string]*EntryCacheInfo),
		Extensions: extensions,
		m:          m,
		rootLen:    len(rootDir)}
}

// Calculate пересчитывает кеш аудио каталогов.
func (ent *Entries) Calculate(dir string) (err error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			ent = nil
		}
	}()
	for _, info := range files {
		if info.IsDir() {
			childDir := filepath.Join(dir, info.Name())
			ent.m[childDir] = info
			err = ent.Calculate(filepath.Join(dir, info.Name()))
			if err != nil {
				return
			}
		} else {
			if ent.isSupportedAudio(info.Name()) {
				err = ent.Add(dir)
				break
			}
		}
	}
	return
}

// Add рекурсивно добавляет аудио каталог и всех его родителей (если необходимо).
func (ent *Entries) Add(audioDir string) error {
	inode, err := ent.Inode(audioDir) // для самого каталога
	if err != nil {
		return err
	}
	ent.Cache[audioDir] = &EntryCacheInfo{Inode: inode}
	dir := audioDir
	for parent := filepath.Dir(dir); len(parent) >= ent.rootLen; {
		if _, ok := ent.Cache[parent]; !ok {
			parentInode, err := ent.Inode(parent) // для каждого из родителей
			if err != nil {
				return err
			}
			ent.Cache[parent] = &EntryCacheInfo{
				ChildDirs: []string{dir},
				Inode:     parentInode}
		}
		if !collection.ContainsStr(dir, ent.Cache[parent].ChildDirs) {
			ent.Cache[parent].ChildDirs = append(ent.Cache[parent].ChildDirs, dir)
		}
		dir = parent
		parent = filepath.Dir(dir)
	}
	return nil
}

// Rename переименовывает аудио или родительский каталог.
// При этом вносятся изменения в списке дочерних каталогов родительского каталога для данного.
func (ent *Entries) Rename(oldPath, newPath string) error {
	if err := ent.renameChildren(oldPath, newPath); err != nil {
		return err
	}
	parent := filepath.Dir(oldPath)
	if _, ok := ent.Cache[parent]; !ok {
		return fmt.Errorf("path not found into entry cache: %s", parent)
	}
	parentEntryInfo := ent.Cache[parent]
	for i := 0; i < len(parentEntryInfo.ChildDirs); i++ {
		if parentEntryInfo.ChildDirs[i] == oldPath {
			parentEntryInfo.ChildDirs[i] = newPath
		}
	}
	return nil
}

func (ent *Entries) renameChildren(oldPath, newPath string) error {
	if _, ok := ent.Cache[oldPath]; !ok {
		return fmt.Errorf("path not found into entry cache: %s", oldPath)
	}
	ent.Cache[newPath] = ent.Cache[oldPath]
	ent.m[newPath] = ent.m[oldPath]
	for i := 0; i < len(ent.Cache[newPath].ChildDirs); i++ {
		childName := filepath.Base(ent.Cache[newPath].ChildDirs[i])
		newChildPath := filepath.Join(newPath, childName)
		if err := ent.renameChildren(ent.Cache[newPath].ChildDirs[i], newChildPath); err != nil {
			return err
		}
		ent.Cache[newPath].ChildDirs[i] = filepath.Join(newPath, childName)
	}
	delete(ent.Cache, oldPath)
	delete(ent.m, oldPath)
	return nil
}

// Delete удаляет аудио или родительский каталог.
// При этом имя каталога также удаляется в дочернем списке родительского каталога для данного.
func (ent *Entries) Delete(dir string) error {
	if _, ok := ent.Cache[dir]; !ok {
		return fmt.Errorf("path not found into cache: %s", dir)
	}
	for _, dirName := range ent.Cache[dir].ChildDirs {
		if err := ent.Delete(filepath.Join(dir, dirName)); err != nil {
			return err
		}
	}
	delete(ent.Cache, dir)
	delete(ent.m, dir)
	return nil
}

// Compare сравнивает два набора кеша.
// Параметр `old` представляет из себя предыдущий снимок данных.
func (ent *Entries) Compare(old *Entries) map[string]EntryModification {
	m := make(map[string]EntryModification)
	for path, entryInfo := range ent.Cache {
		if _, ok := old.Cache[path]; !ok {
			var renamed bool
			for oldPath, oldEntryInfo := range old.Cache {
				if oldEntryInfo.Inode == entryInfo.Inode {
					m[oldPath] = EntryModification{
						Change:  RenamedFsChange,
						NewName: path}
					renamed = true
					break
				}
			}
			if !renamed {
				m[path] = EntryModification{Change: CreatedFsChange}
			}
		}
	}
	for oldPath := range old.Cache {
		if _, ok := ent.Cache[oldPath]; !ok {
			if _, ok := m[oldPath]; !ok {
				m[oldPath] = EntryModification{Change: DeletedFsChange}
			}
		}
	}
	return m
}

// MarshalJSON сериализует данные кеша в JSON.
// func (ent *Entries) MarshalJSON() ([]byte, error) {
// 	return json.Marshal(ent)
// }

// LoadFrom заполняет кеш из JSON.
// Попутно формируется внутренний кеш iNode для каталогов.
func (ent *Entries) LoadFrom(fn string) error {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &ent.Cache); err != nil {
		return err
	}
	for path := range ent.Cache {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		ent.m[path] = info
	}
	return nil
}

// SaveTo сохраняет кеш в указанном файле.
func (ent *Entries) SaveTo(fn string) error {
	data, err := json.Marshal(ent.Cache)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(fn, data, 0644)
}
func (ent *Entries) isSupportedAudio(fn string) bool {
	return collection.ContainsStr(filepath.Ext(fn), ent.Extensions)
}

// Inode возвращает числовое значение каталога в файловой системе.
func (ent *Entries) Inode(path string) (_ uint64, err error) {
	fi, ok := ent.m[path]
	if !ok {
		fi, err = os.Stat(path)
		if err != nil {
			return
		}
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("Not a syscall.Stat_t")
	}
	return stat.Ino, nil
}
