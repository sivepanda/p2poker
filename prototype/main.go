package main

import (
	"fmt"
	"math/big"
	"sync"
)

// N is the total number of nodes in the network. Node 0 is designated as the leader.
// ShuffleGrid[i][j] is the channel for sending the deck from node i to node j.
// DealingGrid[i][j] is the channel for sending card deal requests from node i to node j.
// P is the publicly known prime used for commutative encryption/decryption.
var (
	N           = 4
	ShuffleGrid = make([][]chan [][]byte, N)
	DealingGrid = make([][]chan DealingRequest, N)
	P           = big.NewInt(13619)
)

func main() {
	SetupGrids()

	var wg sync.WaitGroup

	fmt.Printf("[Main] Starting %d nodes (Node 0 is leader)\n", N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		if i == 0 {
			go runLeader(i, &wg)
		} else {
			go runFollower(i, &wg)
		}
	}

	wg.Wait()
	fmt.Println("[Main] All nodes finished.")
}

// runLeader coordinates the shuffle phase, then participates as a player.
func runLeader(id int, wg *sync.WaitGroup) {
	defer wg.Done()

	key := GenerateKey()
	fmt.Printf("[Leader %d] Generated encryption key\n", id)

	finalDeck := shufflePhaseLeader(key)

	fmt.Printf("[Leader %d] Final deck sample (first 5 cards):\n", id)
	for i := 0; i < 5; i++ {
		fmt.Printf("  [Leader %d] Card %d: %s\n", id, i, string(finalDeck[i]))
	}

	runPlayer(id, finalDeck, key)
}

// runFollower participates in the shuffle relay, then becomes a player.
func runFollower(id int, wg *sync.WaitGroup) {
	defer wg.Done()

	key := GenerateKey()
	fmt.Printf("[Follower %d] Generated encryption key\n", id)

	finalDeck := shufflePhaseFollower(id, key)

	fmt.Printf("[Follower %d] Final deck sample (first 2 cards):\n", id)
	for i := 0; i < 2; i++ {
		fmt.Printf("  [Follower %d] Card %d: %s\n", id, i, string(finalDeck[i]))
	}

	runPlayer(id, finalDeck, key)
}

// runPlayer deals cards to itself by circulating a decrypt request around the ring.
// will be later expanded to have actual game logic
func runPlayer(id int, finalDeck [][]byte, key *Key) {
	card1, card2 := dealCards(id, finalDeck, key)
	fmt.Printf("\n[Player %d] Received cards: [%s] [%s]\n", id, card1, card2)
}

// shufflePhaseLeader initializes the deck, encrypts and shuffles it,
// then relays it around the ring of followers. Once it returns,
// the leader broadcasts the fully shuffled deck to all nodes.
//
// Shuffle relay order: Leader -> 1 -> 2 -> ... -> N-1 -> Leader
func shufflePhaseLeader(key *Key) [][]byte {
	// Initialize an ordered deck of 52 cards represented as byte slices
	deck := make([][]byte, 52)
	for i := 0; i < 52; i++ {
		deck[i] = []byte(fmt.Sprintf("%d", i))
	}

	// Leader shuffles and encrypts first before passing to followers
	ShuffleDeck(deck)
	deck = EncryptDeck(deck, key)

	// Send the deck to the first follower to start the relay
	fmt.Printf("\nSHUFFLE PHASE\n\n")
	fmt.Printf("[Leader 0] Initiating shuffle relay -> Node 1\n")
	ShuffleGrid[0][1] <- deck

	// Block until the deck has traveled the full ring and returned
	finalDeck := <-ShuffleGrid[N-1][0]
	fmt.Printf("[Leader 0] Shuffle relay complete, received final deck from Node %d\n", N-1)

	// Broadcast the final shuffled deck to all followers
	fmt.Printf("[Leader 0] Broadcasting final deck to all nodes\n")
	for i := 1; i < N; i++ {
		ShuffleGrid[0][i] <- finalDeck
	}

	return finalDeck
}

// shufflePhaseFollower waits for the deck from the previous node,
// shuffles and encrypts it, then passes it forward in the relay.
// After the relay, it waits for the final deck broadcast from the leader.
func shufflePhaseFollower(id int, key *Key) [][]byte {
	prev := id - 1
	next := (id + 1) % N

	// Wait for the deck from the previous node in the relay
	deck := <-ShuffleGrid[prev][id]
	fmt.Printf("[Follower %d] Received deck from Node %d\n", id, prev)

	// Shuffle and re-encrypt with this node's private key
	ShuffleDeck(deck)
	deck = EncryptDeck(deck, key)

	// Pass the deck forward (last follower sends back to leader)
	fmt.Printf("[Follower %d] Passing deck to Node %d\n", id, next)
	ShuffleGrid[id][next] <- deck

	// Wait for the final deck broadcast from the leader
	finalDeck := <-ShuffleGrid[0][id]
	fmt.Printf("[Follower %d] Received final deck broadcast from Leader\n", id)

	return finalDeck
}

// dealCards handles the dealing phase for a single player.
// Each player's two cards sit at indices 2*id and 2*id+1 in the final deck.
//
// To decrypt a card, all N nodes must apply their decryption key to it.
// This is achieved by circulating a DealingRequest around the ring:
//
//	id -> id+1 -> ... -> N-1 -> 0 -> ... -> id
//
// When the request returns to its origin, it has been decrypted by every node.
func dealCards(id int, finalDeck [][]byte, key *Key) (string, string) {
	cardIndex1 := 2 * id
	cardIndex2 := 2*id + 1

	myRequest := DealingRequest{
		id:     id,
		index1: cardIndex1,
		index2: cardIndex2,
		value1: finalDeck[cardIndex1],
		value2: finalDeck[cardIndex2],
	}

	src := id
	dst := (id + 1) % N
	prev := (N + id - 1) % N

	// Inject this node's request into the ring
	fmt.Printf("[Player %d] Sending deal request for cards [%d, %d] to Node %d\n", id, cardIndex1, cardIndex2, dst)
	DealingGrid[src][dst] <- myRequest

	// Process incoming requests from the previous node in the ring.
	// Each node partially decrypts values as requests pass through.
	// When our own request returns, all nodes have decrypted it.
	for {
		incomingRequest := <-DealingGrid[prev][id]

		// Apply this node's decryption to the card values
		decryptedCard1 := DecryptCard(incomingRequest.value1, key)
		decryptedCard2 := DecryptCard(incomingRequest.value2, key)

		// If this request originated from us, decryption is complete
		if incomingRequest.id == id {
			// TODO: Verify request authenticity — a malicious node could spoof the ID field.
			// Digital signatures would prevent this.
			fmt.Printf("\n[Player %d] Deal request returned fully decrypted", id)
			return string(decryptedCard1), string(decryptedCard2)
		}

		// Forward the partially decrypted request to the next node
		incomingRequest.value1 = decryptedCard1
		incomingRequest.value2 = decryptedCard2
		DealingGrid[src][dst] <- incomingRequest
	}
}
