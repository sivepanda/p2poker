package peer

import (
	"fmt"

	"github.com/sivepanda/p2poker/internal/protocol"
)

func (n *Node) GameStart(frame protocol.Frame) {
	fmt.Printf("[%s] GAME START \n", n.id)
	n.Order = frame.PeerIDs
	for i, id := range n.Order {
		if id == n.id {
			n.SeatIdx = i
			break
		}
	}
	n.Started = true
	n.money = 2000

	if n.onGameStart != nil {
		go n.onGameStart(frame.SessionID, n.Order)
	}

	//Init handlers
	n.InitEphemeralHandlers()
	n.InitShuffleHandlers()
	n.InitDealHandlers()
	n.InitCommunityHandlers()

}

//TODO: Move runner logic in here
