package repokeeper

import (
	"encoding/json"
	"errors"
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

// DirModification описывает изменение конкретного каталога.
type DirModification struct {
	Change  FsChange `json:"change,omitempty"`
	NewName string   `json:"new_name"`
}

// CacheElem описывает каталоговый узел для Album Entry и его родительских каталогов
// в кеше сервиса.
// `Inode` содержит числовой идентификатор каталога в ФС.
// `Modification` содержит последнее обнаруженное изменение. Значение снимается только,
// когда сообщение об изменении было успешно передано подписчику.
// `Children` содержит дочерние пути каталогов, которые ведут к Album Entry.
type CacheElem struct {
	Inode        uint64          `json:"inode"`
	Modification DirModification `json:"modification,omitempty"`
	isAlbumEntry bool
	Children     []string `json:"children,omitempty"`
}

// Entries хранит состояние объекта кеша аудио каталогов.
// В `Cache` хранит только информацию об Album Entry и их родительских каталогах.
type Entries struct {
	Root       string                `json:"root"`
	Cache      map[string]*CacheElem `json:"cache"`
	Extensions []string              `json:"extensions"`
	rootLen    int
}

// NewEntries создает объект для формирования кеша аудио каталогов.
func NewEntries(root string, extensions []string) *Entries {
	return &Entries{
		Root:       root,
		Cache:      make(map[string]*CacheElem),
		Extensions: extensions,
		rootLen:    len(root)}
}

// Calculate пересчитывает кеш аудио каталогов.
func (ent *Entries) Calculate(dir string) (err error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return
	}
	for _, info := range files {
		if info.IsDir() {
			if err = ent.Calculate(filepath.Join(dir, info.Name())); err != nil {
				return
			}
		} else {
			if ent.isSupportedAudio(info.Name()) {
				err = ent.AddAlbumEntry(dir)
				break
			}
		}
	}
	return
}

// Add рекурсивно добавляет аудио каталог и всех его родителей в кеш дерева.
func (ent *Entries) Add(dir string) (*CacheElem, error) {
	if ret, ok := ent.Cache[dir]; ok {
		return ret, nil
	}
	inode, err := Inode(dir)
	if err != nil {
		return nil, err
	}
	ent.Cache[dir] = &CacheElem{Inode: inode}
	parent := filepath.Dir(dir)
	for ; len(parent) >= ent.rootLen; parent = filepath.Dir(dir) {
		if _, ok := ent.Cache[parent]; !ok {
			if _, err := ent.Add(parent); err != nil {
				return nil, err
			}
		}
		if !collection.ContainsStr(dir, ent.Cache[parent].Children) {
			ent.Cache[parent].Children = append(ent.Cache[parent].Children, dir)
		}
		dir = parent
	}
	return ent.Cache[dir], nil
}

// AddAlbumEntry рекурсивно добавляет аудио каталог и всех его родителей в кеш дерева
// и устанавливает признак каталога как Album Entry.
func (ent *Entries) AddAlbumEntry(audioDir string) error {
	elem, err := ent.Add(audioDir)
	if err != nil {
		return err
	}
	elem.isAlbumEntry = true
	return nil
}

// Rename переименовывает каталог.
// Изменения также вносятся в список дочерних каталогов родительского каталога для данного.
func (ent *Entries) Rename(oldDir, newDir string) {
	elem := ent.Cache[oldDir]
	elem.Modification.Change = RenamedFsChange
	elem.Modification.NewName = newDir
	ent.renameChildren(oldDir, newDir)
	parent := filepath.Dir(oldDir)
	if _, ok := ent.Cache[parent]; !ok {
		return
	}
	parentEntryInfo := ent.Cache[parent]
	for i := 0; i < len(parentEntryInfo.Children); i++ {
		if parentEntryInfo.Children[i] == oldDir {
			parentEntryInfo.Children[i] = newDir
		}
	}
}

func (ent *Entries) renameChildren(oldDir, newDir string) {
	ent.Cache[newDir] = ent.Cache[oldDir]
	for i := 0; i < len(ent.Cache[newDir].Children); i++ {
		childName := filepath.Base(ent.Cache[newDir].Children[i])
		newChildPath := filepath.Join(newDir, childName)
		ent.renameChildren(ent.Cache[newDir].Children[i], newChildPath)
		ent.Cache[newDir].Children[i] = filepath.Join(newDir, childName)
	}
	delete(ent.Cache, oldDir)
}

// Delete рекурсивно удаляет каталог.
// Имя каталога также удаляется в дочернем списке родительского каталога для данного.
func (ent *Entries) Delete(dir string) {
	elem := ent.Cache[dir]
	elem.Modification.Change = DeletedFsChange
	for _, dirName := range ent.Cache[dir].Children {
		ent.Delete(filepath.Join(dir, dirName))
	}
	delete(ent.Cache, dir)

	parent := filepath.Dir(dir)
	if _, ok := ent.Cache[parent]; !ok {
		return
	}
	for i := 0; i < len(ent.Cache[parent].Children); i++ {
		if ent.Cache[parent].Children[i] == dir {
			ent.Cache[parent].Children = append(
				ent.Cache[parent].Children[:i],
				ent.Cache[parent].Children[i+1:]...)
			break
		}
	}
}

// ClearChanges очищает сведения об изменении каталога.
func (ent *Entries) ClearChanges(dir string) {
	elem := ent.Cache[dir]
	elem.Modification.Change = 0
	elem.Modification.NewName = ""
}

// Compare сравнивает два набора кеша.
// Параметр `old` представляет из себя предыдущий снимок данных.
// Дополнительно в `old` выбираются вхождения с явно обозначенными изменениями и
// объединяются с результатами.
func (ent *Entries) Compare(old *Entries) map[string]DirModification {
	m := make(map[string]DirModification)
	for path, entryInfo := range ent.Cache {
		if _, ok := old.Cache[path]; !ok {
			var renamed bool
			for oldPath, oldEntryInfo := range old.Cache {
				if oldEntryInfo.Inode == entryInfo.Inode {
					m[oldPath] = DirModification{
						Change:  RenamedFsChange,
						NewName: path}
					renamed = true
					break
				}
			}
			if !renamed {
				m[path] = DirModification{Change: CreatedFsChange}
			}
		}
	}
	for oldPath, elem := range old.Cache {
		if _, ok := ent.Cache[oldPath]; !ok {
			if _, ok := m[oldPath]; !ok {
				m[oldPath] = DirModification{Change: DeletedFsChange}
			}
		} else {
			if elem.Modification.Change != 0 {
				if _, ok := m[oldPath]; !ok {
					m[oldPath] = DirModification{Change: elem.Modification.Change}
				}
			}
		}
	}
	return m
}

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

// IsAlbumEntry проверяет является ли каталог аудиокаталогом.
func (ent *Entries) IsAlbumEntry(dir string) bool {
	elem, ok := ent.Cache[dir]
	if !ok {
		return false
	}
	return elem.isAlbumEntry
}

func (ent *Entries) isSupportedAudio(fn string) bool {
	return collection.ContainsStr(filepath.Ext(fn), ent.Extensions)
}

// Inode возвращает числовое значение каталога в файловой системе.
func Inode(path string) (_ uint64, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	return InodeByInfo(info)
}

// InodeByInfo определяет inode файлового объекта данным в структуре fs.FileInfo.
func InodeByInfo(info fs.FileInfo) (_ uint64, err error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("Not a syscall.Stat_t")
	}
	return stat.Ino, nil
}
