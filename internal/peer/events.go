package peer

import (
	"fmt"
	"time"
)

// Event is a structured log or state-change signal emitted by a node.
// Consumers (e.g. the clientrpc server) subscribe to the node's bus and
// translate these into the proto Event message for frontends.
type Event struct {
	Timestamp time.Time
	NodeID    string
	SessionID string
	Kind      string // "log" | "game_start" | "shuffle" | "deal" | "community" | "round" | "attempt" | "auto_fold"
	Component string
	Message   string
	Fields    map[string]string
	// Cards carries any card values associated with this event — hole cards,
	// community cards, or a snapshot of known cards at emit time. Keys are
	// frontend-friendly labels like "hole1", "hole2", "flop1"..
	Cards map[string]string
}

// Subscribe registers a new listener and returns the channel plus an
// unregister func. Channels are buffered; slow subscribers drop events
// instead of blocking emitters.
func (n *Node) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 128)
	n.busMu.Lock()
	if n.subs == nil {
		n.subs = make(map[chan Event]struct{})
	}
	n.subs[ch] = struct{}{}
	n.busMu.Unlock()

	cancel := func() {
		n.busMu.Lock()
		if _, ok := n.subs[ch]; ok {
			delete(n.subs, ch)
			close(ch)
		}
		n.busMu.Unlock()
	}
	return ch, cancel
}

// Emit delivers an event to every current subscriber. NodeID, SessionID,
// and Timestamp are filled in automatically if the caller left them zero.
func (n *Node) Emit(ev Event) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	if ev.NodeID == "" {
		ev.NodeID = n.ID()
	}
	if ev.SessionID == "" {
		ev.SessionID = n.SessionID()
	}
	if ev.Kind == "" {
		ev.Kind = "log"
	}

	n.busMu.RLock()
	defer n.busMu.RUnlock()
	for ch := range n.subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop rather than block the game loop.
		}
	}
}

// Logf prints to stdout (preserving existing CLI behavior) and emits a
// generic "log" event. component is a free-form tag like "shuffle" or
// "round" that frontends can use to categorize or filter.
func (n *Node) Logf(component, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	n.Emit(Event{
		Kind:      "log",
		Component: component,
		Message:   msg,
	})
}

// EmitKind is a convenience for emitting a typed event with a formatted
// message. Prints to stdout like Logf.
func (n *Node) EmitKind(kind, component, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	n.Emit(Event{
		Kind:      kind,
		Component: component,
		Message:   msg,
	})
}

// EmitCards is like EmitKind but attaches a card-value map (e.g. hole
// cards, flop cards). Use for deal / community events so frontends can
// render the current hand without a follow-up GetCards call.
func (n *Node) EmitCards(kind, component, message string, cards map[string]string) {
	if message != "" {
		fmt.Println(message)
	}
	n.Emit(Event{
		Kind:      kind,
		Component: component,
		Message:   message,
		Cards:     cards,
	})
}

// EmitFields is like EmitKind but carries structured key/value pairs.
func (n *Node) EmitFields(kind, component, message string, fields map[string]string) {
	if message != "" {
		fmt.Println(message)
	}
	n.Emit(Event{
		Kind:      kind,
		Component: component,
		Message:   message,
		Fields:    fields,
	})
}
