package sim

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"

	deckcrypto "github.com/sivepanda/p2poker/internal/crypto/deck"
)

type Config struct {
	NumNodes int
	Prime    *big.Int
}

type DealingRequest struct {
	OwnerID int
	Index1  int
	Index2  int
	Value1  []byte
	Value2  []byte
}

type Network struct {
	numNodes int
	prime    *big.Int

	shuffleGrid [][]chan [][]byte
	dealingGrid [][]chan DealingRequest
}

// NewNetwork validates config and allocates simulation channels.
func NewNetwork(cfg Config) (*Network, error) {
	if cfg.NumNodes < 2 {
		return nil, errors.New("num nodes must be at least 2")
	}

	if cfg.Prime == nil || cfg.Prime.Sign() <= 0 {
		return nil, errors.New("prime must be set")
	}

	network := &Network{
		numNodes:    cfg.NumNodes,
		prime:       new(big.Int).Set(cfg.Prime),
		shuffleGrid: make([][]chan [][]byte, cfg.NumNodes),
		dealingGrid: make([][]chan DealingRequest, cfg.NumNodes),
	}

	network.setupGrids()
	return network, nil
}

// Run starts all simulated nodes and waits for completion.
func (n *Network) Run() error {
	var wg sync.WaitGroup

	fmt.Printf("[Main] Starting %d nodes (Node 0 is leader)\n", n.numNodes)

	for id := 0; id < n.numNodes; id++ {
		wg.Add(1)
		if id == 0 {
			go n.runLeader(id, &wg)
			continue
		}

		go n.runFollower(id, &wg)
	}

	wg.Wait()
	fmt.Println("[Main] All nodes finished.")
	return nil
}

// setupGrids allocates channels for shuffle and dealing relays.
func (n *Network) setupGrids() {
	for i := 0; i < n.numNodes; i++ {
		n.shuffleGrid[i] = make([]chan [][]byte, n.numNodes)
		n.dealingGrid[i] = make([]chan DealingRequest, n.numNodes)

		for j := 0; j < n.numNodes; j++ {
			n.shuffleGrid[i][j] = make(chan [][]byte, 1)
			n.dealingGrid[i][j] = make(chan DealingRequest, 1)
		}
	}
}

// runLeader executes shuffle and dealing for node zero.
func (n *Network) runLeader(id int, wg *sync.WaitGroup) {
	defer wg.Done()

	key, err := deckcrypto.GenerateKey(n.prime)
	if err != nil {
		fmt.Printf("[Leader %d] Key generation failed: %v\n", id, err)
		return
	}

	fmt.Printf("[Leader %d] Generated encryption key\n", id)

	finalDeck, err := n.shufflePhaseLeader(key)
	if err != nil {
		fmt.Printf("[Leader %d] Shuffle phase failed: %v\n", id, err)
		return
	}

	fmt.Printf("[Leader %d] Final deck sample (first 5 cards):\n", id)
	for i := 0; i < 5; i++ {
		fmt.Printf("  [Leader %d] Card %d: %s\n", id, i, string(finalDeck[i]))
	}

	n.runPlayer(id, finalDeck, key)
}

// runFollower executes shuffle and dealing for non-leader nodes.
func (n *Network) runFollower(id int, wg *sync.WaitGroup) {
	defer wg.Done()

	key, err := deckcrypto.GenerateKey(n.prime)
	if err != nil {
		fmt.Printf("[Follower %d] Key generation failed: %v\n", id, err)
		return
	}

	fmt.Printf("[Follower %d] Generated encryption key\n", id)

	finalDeck, err := n.shufflePhaseFollower(id, key)
	if err != nil {
		fmt.Printf("[Follower %d] Shuffle phase failed: %v\n", id, err)
		return
	}

	fmt.Printf("[Follower %d] Final deck sample (first 2 cards):\n", id)
	for i := 0; i < 2; i++ {
		fmt.Printf("  [Follower %d] Card %d: %s\n", id, i, string(finalDeck[i]))
	}

	n.runPlayer(id, finalDeck, key)
}

// runPlayer handles card dealing for one simulated node.
func (n *Network) runPlayer(id int, finalDeck [][]byte, key *deckcrypto.Key) {
	card1, card2 := n.dealCards(id, finalDeck, key)
	fmt.Printf("\n[Player %d] Received cards: [%s] [%s]\n", id, card1, card2)
}

// shufflePhaseLeader starts relay, then broadcasts final deck.
func (n *Network) shufflePhaseLeader(key *deckcrypto.Key) ([][]byte, error) {
	deck := make([][]byte, 52)
	for i := 0; i < len(deck); i++ {
		deck[i] = []byte(strconv.Itoa(i))
	}

	if err := deckcrypto.Shuffle(deck); err != nil {
		return nil, err
	}

	deck = deckcrypto.EncryptDeck(deck, key, n.prime)

	fmt.Printf("\nSHUFFLE PHASE\n\n")
	fmt.Printf("[Leader 0] Initiating shuffle relay -> Node 1\n")
	n.shuffleGrid[0][1] <- deck

	finalDeck := <-n.shuffleGrid[n.numNodes-1][0]
	fmt.Printf("[Leader 0] Shuffle relay complete, received final deck from Node %d\n", n.numNodes-1)

	fmt.Printf("[Leader 0] Broadcasting final deck to all nodes\n")
	for i := 1; i < n.numNodes; i++ {
		n.shuffleGrid[0][i] <- finalDeck
	}

	return finalDeck, nil
}

// shufflePhaseFollower shuffles, encrypts, and forwards the deck.
func (n *Network) shufflePhaseFollower(id int, key *deckcrypto.Key) ([][]byte, error) {
	prev := id - 1
	next := (id + 1) % n.numNodes

	deck := <-n.shuffleGrid[prev][id]
	fmt.Printf("[Follower %d] Received deck from Node %d\n", id, prev)

	if err := deckcrypto.Shuffle(deck); err != nil {
		return nil, err
	}

	deck = deckcrypto.EncryptDeck(deck, key, n.prime)

	fmt.Printf("[Follower %d] Passing deck to Node %d\n", id, next)
	n.shuffleGrid[id][next] <- deck

	finalDeck := <-n.shuffleGrid[0][id]
	fmt.Printf("[Follower %d] Received final deck broadcast from Leader\n", id)

	return finalDeck, nil
}

// dealCards runs the ring decryption flow for this player.
func (n *Network) dealCards(id int, finalDeck [][]byte, key *deckcrypto.Key) (string, string) {
	cardIndex1 := 2 * id
	cardIndex2 := 2*id + 1

	myRequest := DealingRequest{
		OwnerID: id,
		Index1:  cardIndex1,
		Index2:  cardIndex2,
		Value1:  finalDeck[cardIndex1],
		Value2:  finalDeck[cardIndex2],
	}

	src := id
	dst := (id + 1) % n.numNodes
	prev := (n.numNodes + id - 1) % n.numNodes

	fmt.Printf("[Player %d] Sending deal request for cards [%d, %d] to Node %d\n", id, cardIndex1, cardIndex2, dst)
	n.dealingGrid[src][dst] <- myRequest

	for {
		incomingRequest := <-n.dealingGrid[prev][id]

		decryptedCard1 := deckcrypto.DecryptCard(incomingRequest.Value1, key, n.prime)
		decryptedCard2 := deckcrypto.DecryptCard(incomingRequest.Value2, key, n.prime)

		if incomingRequest.OwnerID == id {
			fmt.Printf("\n[Player %d] Deal request returned fully decrypted", id)
			return string(decryptedCard1), string(decryptedCard2)
		}

		incomingRequest.Value1 = decryptedCard1
		incomingRequest.Value2 = decryptedCard2
		n.dealingGrid[src][dst] <- incomingRequest
	}
}
