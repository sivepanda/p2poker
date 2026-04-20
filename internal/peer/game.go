package peer

import (
	"fmt"
	"strings"
	"time"

	"github.com/sivepanda/p2poker/internal/protocol"
)

func (n *Node) GameStart(frame protocol.Frame) {
	order := make([]string, len(frame.PeerIDs))
	copy(order, frame.PeerIDs)

	myID := n.ID()
	seatIdx := -1
	for i, id := range order {
		if id == myID {
			seatIdx = i
			break
		}
	}

	n.mu.Lock()
	n.Order = order
	n.SeatIdx = seatIdx
	n.sessionConfig = SessionConfig{
		TimeoutInterval: time.Duration(frame.TimeoutIntervalMS) * time.Millisecond,
		MaxAttempts:     frame.MaxAttempts,
	}
	n.Started = true
	n.money = 2000
	n.mu.Unlock()

	n.gameStartOnce.Do(func() {
		close(n.gameStartCh)
	})

	n.EmitFields("game_start", "game",
		fmt.Sprintf("[%s] GAME START", myID),
		map[string]string{
			"session_id": frame.SessionID,
			"order":      strings.Join(order, ","),
			"my_seat":    fmt.Sprintf("%d", seatIdx),
		})

	if n.onGameStart != nil {
		go n.onGameStart(frame.SessionID, order)
	}

	n.InitEphemeralHandlers()
}

//TODO: Move runner logic in here
