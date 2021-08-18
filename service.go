package repokeeper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/streadway/amqp"

	md "github.com/ytsiuryn/ds-audiomd"
	srv "github.com/ytsiuryn/ds-microservice"
)

// Константы микросервиса
const (
	ServiceName = "repokeeper"
	CacheFile   = ".cache"
)

// RepoKeeper описывает внутреннее состояние хранителя репозитория.
type RepoKeeper struct {
	*srv.Service
	rootDir           string
	extensions        []string
	pub               *srv.Publisher
	w                 *fsnotify.Watcher
	entries           *Entries
	inodesForRenaming map[string]uint64
}

// New создает объект хранителя репозитория.
// Корневой каталог аудио рпеозитори должен быть указан как абсолютный путь.
func New(rootDir string, extensions []string) *RepoKeeper {
	if !filepath.IsAbs(rootDir) {
		srv.FailOnError(
			errors.New("audio repository root dir must be an absolute path"),
			"service parameters parsing")
	}

	w, err := fsnotify.NewWatcher()
	srv.FailOnError(err, "watcher initialization")

	err = w.Add(rootDir)
	srv.FailOnError(err, "watch point adding")

	return &RepoKeeper{
		Service:           srv.NewService(ServiceName),
		rootDir:           rootDir,
		extensions:        extensions,
		w:                 w,
		entries:           NewEntries(rootDir, extensions),
		inodesForRenaming: make(map[string]uint64)}
}

// AnswerWithError заполняет структуру ответа информацией об ошибке.
func (rk *RepoKeeper) AnswerWithError(delivery *amqp.Delivery, err error, context string) {
	rk.LogOnError(err)
	req := &AudioRepoResponse{
		Error: &srv.ErrorResponse{
			Error:   err.Error(),
			Context: context,
		},
	}
	data, err := json.Marshal(req)
	srv.FailOnError(err, "answer marshalling error")
	rk.Answer(delivery, data)
}

// StartWithConnection запускает Web Poller и цикл обработки входящих запросов.
// Контролирует сигнал завершения цикла и последующего освобождения ресурсов микросервиса.
func (rk *RepoKeeper) StartWithConnection(connstr string) {
	rk.pub = srv.NewPublisher(connstr)
	msgs := rk.Service.ConnectToMessageBroker(connstr)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		rk.applyChangesBetweenSessions()
		rk.addWatchPoints()
		rk.fsEvents()
	}()

	go func() {
		for delivery := range msgs {
			var req AudioRepoRequest
			if err := json.Unmarshal(delivery.Body, &req); err != nil {
				rk.AnswerWithError(&delivery, err, "Message dispatcher")
				continue
			}
			rk.logRequest(&req)
			rk.RunCmd(&req, &delivery)
		}
	}()

	rk.Log.Info("Awaiting RPC requests")
	<-c

	rk.cleanup()
}

func (rk *RepoKeeper) cleanup() {
	if err := rk.entries.SaveTo(CacheFile); err != nil {
		rk.Log.Error(err)
	}
	if err := rk.w.Close(); err != nil {
		rk.Log.Error(err)
	}
	rk.Service.Cleanup()
}

// Отображение сведений о выполняемом запросе.
func (rk *RepoKeeper) logRequest(req *AudioRepoRequest) {
	if len(req.Path) > 0 {
		rk.Log.Infof("%s(%s)", req.Cmd, req.Path)
	} else {
		rk.Log.Infof("%s()", req.Cmd)
	}
}

// RunCmd выполняет команды и возвращает результат клиенту в виде JSON-сообщения.
func (rk *RepoKeeper) RunCmd(req *AudioRepoRequest, delivery *amqp.Delivery) {
	var data []byte
	var err error

	switch req.Cmd {
	case "normalize":
		data, err = rk.normalize(req)
	default:
		rk.Service.RunCmd(req.Cmd, delivery)
		return
	}

	if err != nil {
		rk.AnswerWithError(delivery, err, req.Cmd)
	} else {
		if len(data) > 0 {
			rk.Log.Debug(string(data))
		}
		rk.Answer(delivery, data)
	}
}

func (rk *RepoKeeper) applyChangesBetweenSessions() {
	err := rk.entries.Calculate(rk.rootDir)
	srv.FailOnError(err, "entry cache creation")
	// проведение изменений с момента последнего формирования кеша и по настоящий момент
	oldParents := NewEntries(rk.rootDir, rk.extensions)
	if err := oldParents.LoadFrom(CacheFile); err == nil {
		for path, mod := range rk.entries.Compare(oldParents) {
			switch mod.Change {
			case CreatedFsChange:
				rk.onEntryCreated(path)
			case RenamedFsChange:
				rk.onEntryRenamed(path, mod.NewName)
			case DeletedFsChange:
				rk.onEntryDeleted(path)
			}
		}
	} else {
		if os.IsExist(err) {
			// if err != os.ErrNotExist {
			srv.FailOnError(err, "cache reading")
		}
	}
}

// рекурсивное добавление Entry и их родительских каталогов по данным кеша.
func (rk *RepoKeeper) addWatchPoints() {
	for path := range rk.entries.Cache {
		if rk.isAlbumEntry(path) {
			srv.FailOnError(rk.w.Add(path), fmt.Sprintf("add watch dir: %s", path))
		}
	}
}

func (rk *RepoKeeper) fsEvents() {
	for {
		select {
		case event, ok := <-rk.w.Events:
			if !ok {
				srv.FailOnError(errors.New("event retriving error"), "fsEvents")
			}
			rk.Log.Debug(event)

			info, err := os.Stat(event.Name)
			srv.FailOnError(err, "fsEvents")

			if event.Op&fsnotify.Create == fsnotify.Create {
				rk.onFsObjectCreated(event.Name, info)

			} else if event.Op&fsnotify.Rename == fsnotify.Rename {
				rk.onFsObjectRenamed(event.Name, info)

			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				rk.onFsObjectDeleted(event.Name, info)
			}

		case err, ok := <-rk.w.Errors:
			if !ok {
				srv.FailOnError(errors.New("error retriving error"), "fsEvents")
			}
			rk.Log.Error(err)
		}
	}
}

func (rk *RepoKeeper) onFsObjectCreated(path string, info fs.FileInfo) {
	if info.IsDir() {
		inode, err := InodeByInfo(info)
		srv.FailOnError(err, "inode retrieving")
		for oldName, oldInode := range rk.inodesForRenaming {
			if oldInode == inode {
				if rk.isAlbumEntry(path) {
					rk.onEntryRenamed(oldName, path)
				}
				rk.entries.Rename(oldName, path)
				delete(rk.inodesForRenaming, path)
				break
			}
		}
	} else {
		if rk.entries.isSupportedAudio(path) {
			parent := filepath.Dir(path)
			if _, ok := rk.entries.Cache[parent]; !ok {
				rk.entries.AddAlbumEntry(parent)
				rk.onEntryCreated(path)
			}
		}
	}
}

func (rk *RepoKeeper) onFsObjectRenamed(path string, info fs.FileInfo) {
	if info.IsDir() && rk.isAlbumEntry(path) {
		inode, err := InodeByInfo(info)
		srv.FailOnError(err, "inode retrieving")
		rk.inodesForRenaming[path] = inode
	}
}

func (rk *RepoKeeper) onFsObjectDeleted(path string, info fs.FileInfo) {
	if info.IsDir() {
		if rk.isAlbumEntry(path) {
			rk.onEntryDeleted(path)
		}
		rk.entries.Delete(filepath.Dir(path))
	}
}

func (rk *RepoKeeper) onEntryCreated(path string) {
	rk.pub.Emit("text/plain", []byte("dir created: "+path))
}

func (rk *RepoKeeper) onEntryRenamed(oldPath, newPath string) {
	rk.pub.Emit("text/plain", []byte("dir renamed: "+oldPath+" -> "+newPath))
}

func (rk *RepoKeeper) onEntryDeleted(path string) {
	rk.pub.Emit("text/plain", []byte("dir deleted: "+path))
}

func (rk *RepoKeeper) isAlbumEntry(path string) bool {
	return rk.entries.IsAlbumEntry(path)
}

// нормализация имени каталога, исходя из метаданных альбома
// Из amqp.Delivery извлекаются параметры:
// - нормализовать один каталог с альбомом или все
// - путь к каталогу альбома для единичной нормализации
// - JSON для объекта ds_audiomd.Release
func (rk *RepoKeeper) normalize(req *AudioRepoRequest) (_ []byte, err error) {
	return
}

// IsReadyForNormalization проверка наличия всех необходимых метаданных
// для проведения нормализации.
func IsReadyForNormalization(release md.Release) bool {
	return false
}

// IsNormalized проверяет корректность имени каталога, исходя из метаданных альбома.
func IsNormalized(path string, release md.Release) bool {
	return false
}
