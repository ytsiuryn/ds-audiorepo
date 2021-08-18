package repokeeper

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testExtensions = []string{".flac", ".dsf", ".mp3", ".wv"}

func TestEntriesCalculate(t *testing.T) {
	ent := NewEntries("testdata/repo", testExtensions)
	require.NoError(t, ent.Calculate("testdata/repo"))
	// data, _ := json.Marshal(ent.Cache)
	// fmt.Println(string(data))
	assert.NotContains(t, ent.Cache, "non-audio")
}

func TestEntriesLoad(t *testing.T) {
	ent := NewEntries("testdata/repo", testExtensions)
	require.NoError(t, ent.LoadFrom("testdata/cache"))
	assert.Len(t, ent.Cache, 8)
}

func TestEntriesCompare(t *testing.T) {
	var old, ent *Entries
	old = NewEntries("testdata/repo", testExtensions)
	ent = NewEntries("testdata/repo", testExtensions)
	require.NoError(t, old.LoadFrom("testdata/cache"))
	for k, v := range old.Cache {
		ent.Cache[k] = v
	}

	require.NoError(t, os.Mkdir("testdata/repo/other_mp3", os.ModeDir))
	defer func() { os.Remove("testdata/repo/other_mp3") }()
	// fi, err := os.Stat("testdata/repo/other_mp3")
	// require.NoError(t, err)
	// ent.m["testdata/repo/other_mp3"] = fi
	ent.AddAlbumEntry("testdata/repo/other_mp3")
	ent.Delete("testdata/repo/flac")
	ent.Rename("testdata/repo/wv/wv", "testdata/repo/wv/wv2")
	assert.Len(t, ent.Cache, 8)

	changes := ent.Compare(old)
	assert.Equal(t, changes["testdata/repo/other_mp3"].Change, CreatedFsChange)
	assert.Equal(t, changes["testdata/repo/flac"].Change, DeletedFsChange)
	assert.Equal(t, changes["testdata/repo/wv/wv"].Change, RenamedFsChange)
	assert.Equal(t, changes["testdata/repo/wv/wv"].NewName, "testdata/repo/wv/wv2")
}
