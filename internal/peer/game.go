package peer

import (
	"fmt"
	"strings"
	"time"

	"github.com/sivepanda/p2poker/internal/protocol"
)

func (n *Node) GameStart(frame protocol.Frame) {
	n.Order = frame.PeerIDs
	for i, id := range n.Order {
		if id == n.id {
			n.SeatIdx = i
			break
		}
	}
	n.sessionConfig = SessionConfig{
		TimeoutInterval: time.Duration(frame.TimeoutIntervalMS) * time.Millisecond,
		MaxAttempts:     frame.MaxAttempts,
	}
	n.Started = true
	n.money = 2000

	n.EmitFields("game_start", "game",
		fmt.Sprintf("[%s] GAME START", n.id),
		map[string]string{
			"session_id": frame.SessionID,
			"order":      strings.Join(n.Order, ","),
			"my_seat":    fmt.Sprintf("%d", n.SeatIdx),
		})

	if n.onGameStart != nil {
		go n.onGameStart(frame.SessionID, n.Order)
	}

	//Init handlers
	n.InitEphemeralHandlers()
	n.InitShuffleHandlers()
	n.InitDealHandlers()

}

//TODO: Move runner logic in here
