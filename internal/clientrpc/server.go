package clientrpc

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sivepanda/p2poker/internal/card"
	"github.com/sivepanda/p2poker/internal/clientrpc/clientrpcpb"
	"github.com/sivepanda/p2poker/internal/game"
	"github.com/sivepanda/p2poker/internal/peer"
	"github.com/sivepanda/p2poker/internal/round"
)

type Server struct {
	clientrpcpb.UnimplementedPokerNodeServer
	node *peer.Node

	runnerMu sync.RWMutex
	runner   *round.Runner
}

// NewServer builds a client RPC server tied to the node.
func NewServer(node *peer.Node) *Server {
	return &Server{node: node}
}

// SetRunner registers the active round runner so SubmitAction can reach it.
// Pass nil to clear.
func (s *Server) SetRunner(r *round.Runner) {
	s.runnerMu.Lock()
	s.runner = r
	s.runnerMu.Unlock()
}

// CreateSession "forwards" incoming gRPC request to internal CreateSession
func (s *Server) CreateSession(ctx context.Context, req *clientrpcpb.CreateSessionRequest) (*clientrpcpb.CreateSessionResponse, error) {
	sessionID, err := s.node.CreateSession(ctx, req.SessionId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.CreateSessionResponse{SessionId: sessionID}, nil
}

// JoinSession "forwards" incoming gRPC request to internal JoinSession
func (s *Server) JoinSession(ctx context.Context, req *clientrpcpb.JoinSessionRequest) (*clientrpcpb.JoinSessionResponse, error) {
	if err := s.node.JoinSession(ctx, req.SessionId); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.JoinSessionResponse{SessionId: req.SessionId}, nil
}

// ListPeers proxies the node's view of peer info.
func (s *Server) ListPeers(ctx context.Context, req *clientrpcpb.ListPeersRequest) (*clientrpcpb.ListPeersResponse, error) {
	peers, err := s.node.ListPeers(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	pbPeers := make([]*clientrpcpb.PeerInfo, len(peers))
	for i, p := range peers {
		pbPeers[i] = &clientrpcpb.PeerInfo{Id: p.ID, Addr: p.Addr}
	}
	return &clientrpcpb.ListPeersResponse{Peers: pbPeers}, nil
}

// ListSessions "forwards" incoming gRPC request to internal ListSessions, which report reports the node's known session IDs.
func (s *Server) ListSessions(ctx context.Context, req *clientrpcpb.ListSessionsRequest) (*clientrpcpb.ListSessionsResponse, error) {
	sessions, err := s.node.ListSessions(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.ListSessionsResponse{SessionIds: sessions}, nil
}

// ConnectPeers "forwards" incoming gRPC request to internal ListSessions, which asks the node to dial every dispatch peer.
func (s *Server) ConnectPeers(ctx context.Context, req *clientrpcpb.ConnectPeersRequest) (*clientrpcpb.ConnectPeersResponse, error) {
	if err := s.node.ConnectToPeers(ctx); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.ConnectPeersResponse{}, nil
}

// StartGame asks dispatch to broadcast game_start to the caller's session.
func (s *Server) StartGame(ctx context.Context, req *clientrpcpb.StartGameRequest) (*clientrpcpb.StartGameResponse, error) {
	if err := s.node.StartGame(ctx); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.StartGameResponse{}, nil
}

// SubmitAction submits a gameplay action to the active round runner.
func (s *Server) SubmitAction(ctx context.Context, req *clientrpcpb.SubmitActionRequest) (*clientrpcpb.SubmitActionResponse, error) {
	s.runnerMu.RLock()
	r := s.runner
	s.runnerMu.RUnlock()
	if r == nil {
		return nil, status.Error(codes.FailedPrecondition, "no active round runner")
	}

	var kind game.ActionKind
	switch req.Kind {
	case clientrpcpb.ActionKind_ACTION_KIND_FOLD:
		kind = game.ActionFold
	case clientrpcpb.ActionKind_ACTION_KIND_CHECK:
		kind = game.ActionCheck
	case clientrpcpb.ActionKind_ACTION_KIND_CALL:
		kind = game.ActionCall
	case clientrpcpb.ActionKind_ACTION_KIND_RAISE:
		kind = game.ActionRaise
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown action kind %v", req.Kind)
	}

	if err := r.SubmitAction(game.Action{Kind: kind, Amount: req.Amount}); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &clientrpcpb.SubmitActionResponse{}, nil
}

// GetCards returns this node's hole cards and the flop (first 3 community cards).
func (s *Server) GetCards(ctx context.Context, req *clientrpcpb.GetCardsRequest) (*clientrpcpb.GetCardsResponse, error) {
	c1, c2 := s.node.HoleCards()
	hand := []string{}
	if c1 != "" {
		hand = append(hand, c1)
	}
	if c2 != "" {
		hand = append(hand, c2)
	}
	community := s.node.CommunityCards()
	if len(community) > 3 {
		community = community[:3]
	}
	return &clientrpcpb.GetCardsResponse{Hand: hand, Flop: community}, nil
}

// DescribeCards decodes deck-int wire strings into structured suit/rank info.
func (s *Server) DescribeCards(ctx context.Context, req *clientrpcpb.DescribeCardsRequest) (*clientrpcpb.DescribeCardsResponse, error) {
	cards, err := card.ParseDeckStrings(req.Cards)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out := make([]*clientrpcpb.CardInfo, len(cards))
	for i, c := range cards {
		out[i] = cardInfo(c)
	}
	return &clientrpcpb.DescribeCardsResponse{Cards: out}, nil
}

// EvaluateHand ranks the strongest 5-card hand out of 5..7 deck-int cards.
func (s *Server) EvaluateHand(ctx context.Context, req *clientrpcpb.EvaluateHandRequest) (*clientrpcpb.EvaluateHandResponse, error) {
	cards, err := card.ParseDeckStrings(req.Cards)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	hr, err := card.Best(cards)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	tb := make([]int32, 0, 5)
	for _, r := range hr.Tiebreakers {
		tb = append(tb, int32(r))
	}
	best := make([]*clientrpcpb.CardInfo, 5)
	for i, c := range hr.Best {
		best[i] = cardInfo(c)
	}
	return &clientrpcpb.EvaluateHandResponse{
		Category:     int32(hr.Category),
		CategoryName: hr.Category.String(),
		Description:  hr.Describe(),
		Tiebreakers:  tb,
		Best:         best,
	}, nil
}

func cardInfo(c card.Card) *clientrpcpb.CardInfo {
	return &clientrpcpb.CardInfo{
		Index:    int32(c.Int()),
		Suit:     int32(c.Suit),
		Rank:     int32(c.Rank),
		Short:    c.Short(),
		Pretty:   c.Pretty(),
		SuitName: c.Suit.Name(),
		RankName: c.Rank.Name(),
	}
}

// GetNodeInfo "forwards" incoming gRPC request to internal ListSessions, which returns basic identities for the node.
func (s *Server) GetNodeInfo(ctx context.Context, req *clientrpcpb.GetNodeInfoRequest) (*clientrpcpb.GetNodeInfoResponse, error) {
	return &clientrpcpb.GetNodeInfoResponse{
		NodeId:     s.node.ID(),
		ListenAddr: s.node.ListenAddr(),
		SessionId:  s.node.SessionID(),
		Attached:   s.node.IsAttached(),
	}, nil
}

// AttachDispatch dials the given dispatch server and registers this node.
// Returns FailedPrecondition if the node is already attached.
func (s *Server) AttachDispatch(ctx context.Context, req *clientrpcpb.AttachDispatchRequest) (*clientrpcpb.AttachDispatchResponse, error) {
	if err := s.node.AttachDispatch(ctx, req.DispatchAddr); err != nil {
		if errors.Is(err, peer.ErrAlreadyAttached) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.AttachDispatchResponse{NodeId: s.node.ID()}, nil
}

// DetachDispatch closes the dispatch connection and tears down the peer mesh.
func (s *Server) DetachDispatch(ctx context.Context, req *clientrpcpb.DetachDispatchRequest) (*clientrpcpb.DetachDispatchResponse, error) {
	if err := s.node.DetachDispatch(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &clientrpcpb.DetachDispatchResponse{}, nil
}

// SubscribeEvents streams node-emitted events (shuffle/deal/round/auto-fold
// progress, sim harness chatter, etc.) plus any incoming "chat" peer
// messages to the client. The frontend sees exactly the signals that used
// to be dumped to stdout.
func (s *Server) SubscribeEvents(req *clientrpcpb.SubscribeEventsRequest, stream grpc.ServerStreamingServer[clientrpcpb.Event]) error {
	ch := make(chan *clientrpcpb.Event, 128)

	s.node.Handle("chat", func(msg peer.Message) {
		select {
		case ch <- &clientrpcpb.Event{
			Event: &clientrpcpb.Event_PeerMessage{
				PeerMessage: &clientrpcpb.PeerMessageEvent{
					From:        msg.From,
					MessageType: msg.Type,
					Payload:     msg.Payload,
				},
			},
		}:
		default:
		}
	})

	busCh, unsubscribe := s.node.Subscribe()
	defer unsubscribe()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-busCh:
			if !ok {
				return nil
			}
			if err := stream.Send(translateBusEvent(ev)); err != nil {
				return err
			}
		case ev := <-ch:
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// translateBusEvent maps a neutral peer.Event to the most specific proto
// Event variant possible. For recognized game-state kinds the frontend gets
// a typed message with parsed fields; anything else falls through to a
// generic LogEvent carrying message + fields + cards maps.
func translateBusEvent(ev peer.Event) *clientrpcpb.Event {
	switch ev.Kind {
	case "game_start":
		return &clientrpcpb.Event{Event: &clientrpcpb.Event_GameStart{
			GameStart: &clientrpcpb.GameStartEvent{
				NodeId:    ev.NodeID,
				SessionId: firstNonEmpty(ev.SessionID, ev.Fields["session_id"]),
				Order:     splitCSV(ev.Fields["order"]),
				MySeat:    atoi32(ev.Fields["my_seat"]),
			},
		}}
	case "deal":
		if len(ev.Cards) > 0 && (ev.Cards["hole1"] != "" || ev.Cards["hole2"] != "") {
			return &clientrpcpb.Event{Event: &clientrpcpb.Event_HoleCards{
				HoleCards: &clientrpcpb.HoleCardsEvent{
					NodeId: ev.NodeID,
					Card1:  ev.Cards["hole1"],
					Card2:  ev.Cards["hole2"],
				},
			}}
		}
	case "community":
		if len(ev.Cards) > 0 {
			return &clientrpcpb.Event{Event: &clientrpcpb.Event_CommunityCards{
				CommunityCards: &clientrpcpb.CommunityCardsEvent{
					NodeId: ev.NodeID,
					Cards:  orderedCardList(ev.Cards),
				},
			}}
		}
	case "round":
		return &clientrpcpb.Event{Event: &clientrpcpb.Event_RoundStarted{
			RoundStarted: &clientrpcpb.RoundStartedEvent{
				NodeId:       ev.NodeID,
				RoundId:      atou64(ev.Fields["round_id"]),
				ProposerSeat: atou32(ev.Fields["proposer_seat"]),
				MySeat:       atoi32(ev.Fields["my_seat"]),
			},
		}}
	case "attempt":
		return &clientrpcpb.Event{Event: &clientrpcpb.Event_Action{
			Action: &clientrpcpb.ActionEvent{
				NodeId:  ev.NodeID,
				RoundId: atou64(ev.Fields["round_id"]),
				Attempt: atou32(ev.Fields["attempt"]),
				Seat:    atoi32(ev.Fields["seat"]),
				Kind:    actionKindFromName(ev.Fields["action_kind"]),
				Amount:  atou64(ev.Fields["action_amount"]),
				Outcome: ev.Fields["outcome"],
				Reason:  ev.Fields["reason"],
			},
		}}
	case "auto_fold":
		if ev.Fields["target_seat"] != "" {
			return &clientrpcpb.Event{Event: &clientrpcpb.Event_AutoFold{
				AutoFold: &clientrpcpb.AutoFoldEvent{
					NodeId:     ev.NodeID,
					RoundId:    atou64(ev.Fields["round_id"]),
					TargetSeat: atou32(ev.Fields["target_seat"]),
				},
			}}
		}
	}
	// Fallback: generic log (free-form shuffle chatter, auto-fold progress,
	// sim harness messages).
	return &clientrpcpb.Event{Event: &clientrpcpb.Event_Log{
		Log: &clientrpcpb.LogEvent{
			TimestampNs: ev.Timestamp.UnixNano(),
			NodeId:      ev.NodeID,
			SessionId:   ev.SessionID,
			Kind:        ev.Kind,
			Component:   ev.Component,
			Message:     ev.Message,
			Fields:      ev.Fields,
			Cards:       ev.Cards,
		},
	}}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func atou64(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

func atou32(s string) uint32 {
	v, _ := strconv.ParseUint(s, 10, 32)
	return uint32(v)
}

func atoi32(s string) int32 {
	v, _ := strconv.ParseInt(s, 10, 32)
	return int32(v)
}

// orderedCardList returns cards["card1"], cards["card2"], ... in order,
// stopping at the first gap so partial reveals (e.g. flop only) still
// render contiguously.
func orderedCardList(cards map[string]string) []string {
	out := make([]string, 0, len(cards))
	for i := 1; ; i++ {
		v, ok := cards["card"+strconv.Itoa(i)]
		if !ok {
			break
		}
		out = append(out, v)
	}
	return out
}

func actionKindFromName(name string) clientrpcpb.ActionKind {
	switch name {
	case "fold":
		return clientrpcpb.ActionKind_ACTION_KIND_FOLD
	case "check":
		return clientrpcpb.ActionKind_ACTION_KIND_CHECK
	case "call":
		return clientrpcpb.ActionKind_ACTION_KIND_CALL
	case "raise":
		return clientrpcpb.ActionKind_ACTION_KIND_RAISE
	default:
		return clientrpcpb.ActionKind_ACTION_KIND_FOLD
	}
}

// Run starts the gRPC server on the given address with a fresh Server.
func Run(ctx context.Context, addr string, node *peer.Node) error {
	return Serve(ctx, addr, NewServer(node))
}

// Serve starts the gRPC server using the given Server instance.
// Use this when the caller needs to hold a reference (e.g. to call SetRunner).
func Serve(ctx context.Context, addr string, s *Server) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := grpc.NewServer()
	clientrpcpb.RegisterPokerNodeServer(srv, s)

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	return srv.Serve(lis)
}
