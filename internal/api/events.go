package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Event is a player event pushed over the /events WebSocket.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// StreamEvents connects to the daemon's event WebSocket and calls onEvent for
// every event. It reconnects with a backoff until ctx is cancelled and reports
// connection changes through onConn.
func (c *Client) StreamEvents(ctx context.Context, onEvent func(Event), onConn func(connected bool)) {
	wsURL := "ws" + strings.TrimPrefix(c.base, "http") + "/events"
	backoff := time.Second
	for ctx.Err() == nil {
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err == nil {
			backoff = time.Second
			onConn(true)
			c.readEvents(ctx, conn, onEvent)
			conn.CloseNow()
			onConn(false)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

func (c *Client) readEvents(ctx context.Context, conn *websocket.Conn, onEvent func(Event)) {
	conn.SetReadLimit(1 << 20)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var ev Event
		if json.Unmarshal(data, &ev) == nil {
			onEvent(ev)
		}
	}
}
