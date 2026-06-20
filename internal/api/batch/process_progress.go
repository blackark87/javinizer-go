package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

func broadcastProgress(msg *ws.ProgressMessage) {
	runtime := core.DefaultRuntimeState()
	if runtime == nil || runtime.WebSocketHub() == nil {
		return
	}
	if err := runtime.WebSocketHub().BroadcastProgress(msg); err != nil {
		logging.Warnf("Failed to broadcast progress update for job %s: %v", msg.JobID, err)
	}
}

func broadcastAlert(severity, message string) {
	runtime := core.DefaultRuntimeState()
	if runtime == nil || runtime.WebSocketHub() == nil {
		return
	}
	if err := runtime.WebSocketHub().BroadcastAlert(severity, message); err != nil {
		logging.Warnf("Failed to broadcast alert: %v", err)
	}
}
