package repokeeper

import (
	"encoding/json"
	"errors"

	"github.com/gofrs/uuid"

	srv "github.com/ytsiuryn/ds-microservice"
)

// AudioRepoRequest описывает формат запроса к менеджеру БД для аудио метаданных.
type AudioRepoRequest struct {
	Cmd  string `json:"cmd"`
	Path string `json:"path,omitempty"`
}

// AudioRepoResponse описывает формат запроса к менеджеру БД для аудио метаданных.
type AudioRepoResponse struct {
	*AudioRepoRequest
	Error *srv.ErrorResponse `json:"error,omitempty"`
}

// Unwrap проверяется возвращает ли ответ описание ошибки и, если она есть,
// выводится сообщение об ощибке по данным ответа и процесс с клиентом останавливается.
func (resp *AudioRepoResponse) Unwrap() *AudioRepoRequest {
	if resp.Error != nil {
		srv.FailOnError(errors.New(resp.Error.Error), resp.Error.Context)
	}
	return resp.AudioRepoRequest
}

// CreateRepoRequest формирует данные запроса по репозиторию.
func CreateRepoRequest(cmd string) (string, []byte, error) {
	correlationID, _ := uuid.NewV4()
	req := AudioRepoRequest{Cmd: cmd}
	data, err := json.Marshal(&req)
	if err != nil {
		return "", nil, err
	}
	return correlationID.String(), data, nil
}

// CreateEntryRequest формирует данные запроса по каталогу альбома.
func CreateEntryRequest(cmd, path string) (string, []byte, error) {
	correlationID, _ := uuid.NewV4()
	req := AudioRepoRequest{Cmd: cmd, Path: path}
	data, err := json.Marshal(&req)
	if err != nil {
		return "", nil, err
	}
	return correlationID.String(), data, nil
}

// ParseRepoAnswer разбирает ответ по репозиторию.
func ParseRepoAnswer(data []byte) (*AudioRepoRequest, error) {
	req := AudioRepoRequest{}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}
