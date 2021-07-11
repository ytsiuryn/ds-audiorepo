package repokeeper

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/streadway/amqp"

	md "github.com/ytsiuryn/ds-audiomd"
	srv "github.com/ytsiuryn/ds-microservice"
)

const ServiceName = "repokeeper"

type entryProperty struct {
	LastUpdate int64    `json:"last_update,omitempty"`
	Status     FSStatus `json:"status"`
	Normalized bool     `json:"normalized,omitempty"`
}

// RepoKeeper описывает внутреннее состояние хранителя репозитория.
type RepoKeeper struct {
	*srv.Service
	rootDir    string
	extensions []string
	cacheFile  string
	entries    map[Path]entryProperty
	lastUpdate time.Time
}

// New создает объект хранителя репозитория.
func New(rootDir string, extensions []string) *RepoKeeper {
	return &RepoKeeper{
		Service:    srv.NewService(ServiceName),
		rootDir:    rootDir,
		extensions: extensions,
		cacheFile:  ".cache",
		entries:    map[Path]entryProperty{},
	}
}

// Start запускает Web Poller и цикл обработки взодящих запросов.
// Контролирует сигнал завершения цикла и последующего освобождения ресурсов микросервиса.
func (rk *RepoKeeper) Start(msgs <-chan amqp.Delivery) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

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
	rk.Service.Cleanup()
}

// Отображение сведений о выполняемом запросе.
func (rk *RepoKeeper) logRequest(req *AudioRepoRequest) {
	if len(req.Path) > 0 {
		rk.Log.WithField("args", req.Path).Info(req.Cmd + "()")
	} else {
		rk.Log.Info(req.Cmd + "()")
	}
}

// RunCmd выполняет команды и возвращает результат клиенту в виде JSON-сообщения.
func (rk *RepoKeeper) RunCmd(req *AudioRepoRequest, delivery *amqp.Delivery) {
	switch req.Cmd {
	case "update":
		rk.updateEntryList(req.FullList, delivery)
	case "normalize":
		rk.normalize(req.Path, delivery)
	default:
		rk.Service.RunCmd(req.Cmd, delivery)
	}
}

func (rk *RepoKeeper) LoadCache() {
	rk.entries = map[Path]entryProperty{}
	data, err := os.ReadFile(rk.cacheFile)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			if os.WriteFile(rk.cacheFile, []byte{}, 0644) != nil {
				rk.Log.WithField("error", err).Error("Cache creating")
			}
		} else {
			rk.Log.WithField("error", err).Error("Cache reading")
		}
		return
	}
	if len(data) == 0 {
		rk.Log.Warn("Cache file is empty")
		return
	}
	if err := json.Unmarshal(data, &rk.entries); err != nil {
		rk.Log.WithField("error", err).Error("Cache unmarshalling")
	}
}

// Создание/обновление кеша списка каталогов альбомов и их свойств.
// Каталог альбомов трактуется по наличию файлов с поддерживаемыми системой аудиофайлами
// выполняется сравнение фактического состояния с кэшем состояния, полученным в результате
// последней проверки.
func (rk *RepoKeeper) updateEntryList(fullList bool, delivery *amqp.Delivery) {
	entries := map[Path]int64{}
	diff := map[Path]entryProperty{}
	haveChanges := false
	if err := rk.albumEntries(rk.rootDir, entries); err != nil {
		rk.AnswerWithError(delivery, err, "Getting entries")
		return
	}
	prop := entryProperty{}
	for cachePath := range rk.entries {
		nsecs, ok := entries[cachePath]
		if !ok {
			prop.Status = DeletedFSStatus
			prop.LastUpdate = 0
		} else {
			if rk.entries[cachePath].LastUpdate < nsecs {
				prop.Status = UpdatedFSStatus
			} else {
				prop.Status = 0
			}
			prop.LastUpdate = nsecs
		}
		if prop.Status != 0 {
			diff[cachePath] = prop
			haveChanges = true
		}
	}
	for path, nsecs := range entries {
		if _, ok := rk.entries[path]; !ok {
			prop.Status = CreatedFSStatus
			prop.LastUpdate = nsecs
			diff[path] = prop
			haveChanges = true
		}
	}
	for path, prop := range diff {
		rk.entries[path] = prop
	}
	var out map[Path]entryProperty
	if fullList {
		out = rk.entries
	} else {
		out = diff
	}
	defer func() {
		for path, prop := range rk.entries {
			if prop.Status == DeletedFSStatus {
				delete(rk.entries, path)
			}
		}
	}()
	answerJSON, err := json.Marshal(out)
	if err != nil {
		rk.AnswerWithError(delivery, err, "Response")
		return
	}
	if haveChanges {
		go func() {
			ioutil.WriteFile(rk.cacheFile, answerJSON, 0x644)
		}()
	}
	rk.Answer(delivery, answerJSON)
}

// нормализация имени каталога, исходя из метаданных альбома
// Из amqp.Delivery извлекаются параметры:
// - нормализовать один каталог с альбомом или все
// - путь к каталогу альбома для единичной нормализации
// - JSON для объекта ds_audiomd.Release
func (rk *RepoKeeper) normalize(path string, delivery *amqp.Delivery) {
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
