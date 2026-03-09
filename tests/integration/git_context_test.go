package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

func TestCommandStarted_StoresGitBranch(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	sessionID := generateSessionID()

	_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test/repo",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "zsh",
			Os:    "linux",
		},
	})
	require.NoError(t, err)

	_, err = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId:   sessionID,
		CommandId:   "cmd-git-branch",
		Cwd:         "/home/test/repo",
		Command:     "git status",
		TsUnixMs:    time.Now().UnixMilli(),
		GitBranch:   "main",
		GitRepoName: "clai",
		GitRepoRoot: "/home/test/repo",
	})
	require.NoError(t, err)

	commands, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     5,
	})
	require.NoError(t, err)
	require.Len(t, commands, 1)
	require.NotNil(t, commands[0].GitBranch)
	require.Equal(t, "main", *commands[0].GitBranch)
}
