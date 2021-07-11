package repokeeper

import (
	"encoding/json"
)

// Тип изменений каталогов альбомов как элементов файловой системы.
type FSStatus int8

// Допустимые значения изменений каталогов альбомов.
const (
	CreatedFSStatus FSStatus = iota + 1
	UpdatedFSStatus
	DeletedFSStatus
)

var fsStatusToStr = map[FSStatus]string{
	CreatedFSStatus: "created",
	UpdatedFSStatus: "updated",
	DeletedFSStatus: "deleted",
}

var strToFSStatus = map[string]FSStatus{
	"created": CreatedFSStatus,
	"updated": UpdatedFSStatus,
	"deleted": DeletedFSStatus,
}

// MarshalJSON преобразует значение статуса каталога к JSON формату.
func (fst FSStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(fsStatusToStr[fst])
}

// UnmarshalJSON получает статус каталога из значения JSON.
func (fst *FSStatus) UnmarshalJSON(b []byte) error {
	k := string(b)
	*fst = strToFSStatus[k[1:len(k)-1]]
	return nil
}
