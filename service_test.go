package repokeeper

import (
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	srv "github.com/ytsiuryn/ds-microservice"
)

func TestAudioRepoKeeper(t *testing.T) {
	// setup code
	startTestService("testdata/repo")
	cl := srv.NewRPCClient()
	defer cl.Close()

	// tests

	t.Run("BaseServiceCommands", func(t *testing.T) {
		correlationID, data, err := srv.CreateCmdRequest("ping")
		require.NoError(t, err)
		cl.Request(ServiceName, correlationID, data)
		respData := cl.Result(correlationID)
		assert.Empty(t, respData)

		correlationID, data, err = srv.CreateCmdRequest("x")
		require.NoError(t, err)
		cl.Request(ServiceName, correlationID, data)
		resp, err := srv.ParseErrorAnswer(cl.Result(correlationID))
		require.NoError(t, err)
		// {"error": "Unknown command: x", "context": "Message dispatcher"}
		assert.NotEmpty(t, resp.Error)
	})
}

func startTestService(dir string) {
	curDir, _ := filepath.Abs(dir)
	testService := New(curDir, []string{".mp3", ".flac", ".dsf", ".wv"})
	msgs := testService.ConnectToMessageBroker("amqp://guest:guest@localhost:5672/")
	testService.Log.SetLevel(log.DebugLevel)
	go testService.Start(msgs)
}
