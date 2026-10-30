package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCtlGuards(t *testing.T) {
	require.Error(t, checkSendText("!git push --force"))
	require.Error(t, checkSendText("/permissions add Bash(*)"))
	require.NoError(t, checkSendText("/devkit:run qa PRD-x"))
	require.NoError(t, checkSendText("продолжай"))
	require.NoError(t, checkProgram("~/.claude-squad/bin/conductor/claude"))
	require.NoError(t, checkProgram("/opt/homebrew/bin/claude --model sonnet"))
	require.Error(t, checkProgram("bash -c 'git push'"))
	require.Error(t, checkProgram("claude --dangerously-skip-permissions"))
	require.Error(t, checkProgram("claude; rm -rf ~"))
	require.Error(t, checkProgram("rm -rf /tmp/x"))
	require.Error(t, checkProgram("sh -c x"))
	require.Error(t, checkProgram("claude --settings /tmp/allow-all.json"))
	require.Error(t, checkProgram("claude --mcp-config /tmp/x.json"))
	require.NoError(t, checkProgram("~/bin/claude-qa"))
	require.Error(t, checkSendText(" !rm -rf x"))
	require.Error(t, checkSendText("ok\r!rm -rf x"))
	require.Error(t, checkSendText("/plugin install x"))
	require.Error(t, checkSendText("/add-dir /"))
}
