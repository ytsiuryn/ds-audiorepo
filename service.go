package repokeeper

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/streadway/amqp"

	dbm "github.com/ytsiuryn/ds-audiodbm"
	ent "github.com/ytsiuryn/ds-audiodbm/entity"
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
	rootDir    string
	extensions []string
	cl         *srv.RPCClient
	w          *fsnotify.Watcher
	entries    *Entries
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
		Service:    srv.NewService(ServiceName),
		cl:         srv.NewRPCClient(),
		rootDir:    rootDir,
		extensions: extensions,
		w:          w,
		entries:    NewEntries(rootDir, extensions)}
}

// AnswerWithError заполняет структуру ответа информацией об ошибке.
func (rk *RepoKeeper) AnswerWithError(delivery *amqp.Delivery, err error, context string) {
	rk.LogOnError(err, context)
	req := &AudioRepoRequest{
		Error: &srv.ErrorResponse{
			Error:   err.Error(),
			Context: context,
		},
	}
	data, err := json.Marshal(req)
	srv.FailOnError(err, "answer marshalling error")
	rk.Answer(delivery, data)
}

// Start запускает Web Poller и цикл обработки входящих запросов.
// Контролирует сигнал завершения цикла и последующего освобождения ресурсов микросервиса.
func (rk *RepoKeeper) Start(msgs <-chan amqp.Delivery) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// контроль изменений между запусками и проведение их на БД
	go func() {
		err := rk.entries.Calculate(rk.rootDir)
		srv.FailOnError(err, "entry cache creation")
		// проведение изменений с момента последнего формирования кеша и по настоящий момент
		oldParents := NewEntries(rk.rootDir, rk.extensions)
		if err := oldParents.LoadFrom(CacheFile); err == nil {
			for path, mod := range rk.entries.Compare(oldParents) {
				switch mod.Change {
				case CreatedFsChange:
					rk.createEntry(path)
				case RenamedFsChange:
					rk.renameEntry(path, mod.NewName)
				case DeletedFsChange:
					rk.deleteEntry(path)
				}
			}
		} else {
			if os.IsExist(err) {
				// if err != os.ErrNotExist {
				srv.FailOnError(err, "cache reading")
			}
		}
		rk.fsEvents()
	}()

	// обработка клиентских запросов
	go func() {
		for delivery := range msgs {
			rk.Log.Info("Новое сообщение")
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

func (rk *RepoKeeper) fsEvents() {
	for {
		select {
		// TODO: обрабатывать ситуации переноса многодисковых треков в общий верхний каталог
		// TODO: а что будет при рекурсивном удалении?
		case event, ok := <-rk.w.Events:
			if !ok {
				rk.Log.Error("fsnotify event retriving error")
				return
			}
			rk.Log.Debug("event:", event)
			if event.Op&fsnotify.Create == fsnotify.Create {
				rk.createEntry(event.Name)
			} else if event.Op&fsnotify.Rename == fsnotify.Rename {
				// rk.renameEntry(event.Name, "")
			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				rk.deleteEntry(event.Name)
			} else {
				rk.Log.Debug("unprocessed event: ", event)
			}
		case err, ok := <-rk.w.Errors:
			if !ok {
				rk.Log.Error("fsnotify error retriving error")
				return
			}
			rk.Log.Error(err)
		}
	}
}

func (rk *RepoKeeper) createEntry(path string) {
	if rk.isAlbumEntry(path) {
		req := dbm.NewAudioDBRequest("set_entry", &ent.AlbumEntry{Path: path})
		rk.dbmRequestAnswer(req)
		rk.Log.Debug("album entry created: ", path)
	}
}

func (rk *RepoKeeper) renameEntry(oldPath, newPath string) {
	if rk.isAlbumEntry(oldPath) {
		req := dbm.NewAudioDBRequest("rename_entry", &ent.AlbumEntry{Path: oldPath})
		req.NewPath = newPath
		rk.dbmRequestAnswer(req)
		rk.Log.Debug("album entry renamed: ", oldPath)
	}
}

func (rk *RepoKeeper) deleteEntry(path string) {
	if rk.isAlbumEntry(path) {
		req := dbm.NewAudioDBRequest("delete_entry", &ent.AlbumEntry{Path: path})
		rk.dbmRequestAnswer(req)
		rk.Log.Debug("album entry deleted: ", path)
	}
}

func (rk *RepoKeeper) dbmRequestAnswer(req *dbm.AudioDBRequest) {
	corrID, data, err := req.Create()
	srv.FailOnError(err, "dbm request sending")
	rk.cl.Request(dbm.ServiceName, corrID, data)
	resp, err := srv.ParseErrorAnswer(rk.cl.Result(corrID))
	srv.FailOnError(err, "dbm response receiving")
	if resp.Error != "" {
		srv.FailOnError(err, "dbm request processing")
	}
}

// в случае ошибки каталог воспринимается не как аудио каталог для блокировки действий по нему
func (rk *RepoKeeper) isAlbumEntry(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		rk.Log.Error(err)
		return false
	}
	if !fi.IsDir() {
		return false
	}
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return false
	}
	for _, info := range files {
		if !info.IsDir() {
			if rk.entries.isSupportedAudio(info.Name()) {
				return true
			}
		}
	}
	return false
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
