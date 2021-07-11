package repokeeper

import (
	"encoding/json"

	"github.com/gofrs/uuid"
)

type AudioRepoRequest struct {
	Cmd      string `json:"cmd"`
	Path     string `json:"path"`
	FullList bool   `json:"full_list"`
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
func ParseRepoAnswer(data []byte) (map[Path]entryProperty, error) {
	entries := map[Path]entryProperty{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
