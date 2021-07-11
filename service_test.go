package repokeeper

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	srv "github.com/ytsiuryn/ds-microservice"
)

var mut sync.Mutex
var testService *RepoKeeper

func TestBaseServiceCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTestService(ctx)

	cl := srv.NewRPCClient()
	defer cl.Close()

	correlationID, data, _ := srv.CreateCmdRequest("ping")
	cl.Request(ServiceName, correlationID, data)
	respData := cl.Result(correlationID)
	if len(respData) != 0 {
		t.Fail()
	}

	correlationID, data, _ = srv.CreateCmdRequest("x")
	cl.Request(ServiceName, correlationID, data)
	vInfo, _ := srv.ParseErrorAnswer(cl.Result(correlationID))
	// {"error": "Unknown command: x", "context": "Message dispatcher"}
	if vInfo.Error != "Unknown command: x" {
		t.Fail()
	}
}

func TestLoadCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTestService(ctx)

	cl := srv.NewRPCClient()
	defer cl.Close()

	curDir, _ := os.Getwd()
	testService.rootDir = curDir
	defer func() {
		os.Remove(testService.cacheFile)
	}()
	os.Remove(testService.cacheFile)
	testService.LoadCache()
	if len(testService.entries) != 0 {
		t.Fail()
	}
	content := `{".":{"updated":0,"status":"created"}}`
	os.WriteFile(testService.cacheFile, []byte(content), 0644)
	testService.LoadCache()
	if len(testService.entries) != 1 && testService.entries["."].Status != CreatedFSStatus {
		t.Fail()
	}
}

func TestUpdateEntryList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTestService(ctx)

	cl := srv.NewRPCClient()
	defer cl.Close()

	defer func() {
		os.Remove(testService.cacheFile)
	}()

	testService.entries = map[Path]entryProperty{}
	testService.entries[Path("bla-bla-path")] = entryProperty{Status: CreatedFSStatus}
	correlationID, data, _ := CreateRepoRequest("update")
	cl.Request(ServiceName, correlationID, data)
	entries, _ := ParseRepoAnswer(cl.Result(correlationID))
	if len(entries) != 1 || entries[Path("bla-bla-path")].Status != DeletedFSStatus {
		t.Fatal("after an entry deleting error")
	}
	os.Create("test.flac")
	defer os.Remove("test.flac")
	correlationID, data, _ = CreateRepoRequest("update")
	cl.Request(ServiceName, correlationID, data)
	answer := cl.Result(correlationID)
	entries, _ = ParseRepoAnswer(answer)
	if len(entries) != 1 || entries[Path(testService.rootDir)].Status != CreatedFSStatus {
		t.Fatal("after a file creating error")
	}
	os.WriteFile("test.flac", []byte{0}, 0644)
	correlationID, data, _ = CreateRepoRequest("update")
	cl.Request(ServiceName, correlationID, data)
	entries, _ = ParseRepoAnswer(cl.Result(correlationID))
	if len(entries) != 1 || entries[Path(testService.rootDir)].Status != UpdatedFSStatus {
		t.Fatal("after a file updating error")
	}
	correlationID, data, _ = CreateRepoRequest("update")
	cl.Request(ServiceName, correlationID, data)
	entries, _ = ParseRepoAnswer(cl.Result(correlationID))
	time.Sleep(time.Millisecond)
	if len(entries) != 0 {
		t.Fatal("without file changes error")
	}
}

func startTestService(ctx context.Context) {
	mut.Lock()
	defer mut.Unlock()
	if testService == nil {
		testService = New(
			".",
			[]string{".mp3", ".flac", ".dsf", ".wv"})
		msgs := testService.ConnectToMessageBroker("amqp://guest:guest@localhost:5672/")
		testService.cacheFile = ".cacheTest"
		// testService.Log.SetLevel(log.DebugLevel)
		curdir, _ := filepath.Abs(".")
		testService.rootDir = curdir
		go testService.Start(msgs)
	}
}
