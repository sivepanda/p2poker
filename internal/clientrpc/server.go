package clientrpc

import (
	"context"
	"errors"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sivepanda/p2poker/internal/clientrpc/clientrpcpb"
	"github.com/sivepanda/p2poker/internal/peer"
)

type Server struct {
	clientrpcpb.UnimplementedPokerNodeServer
	node *peer.Node
}

// NewServer builds a client RPC server tied to the node.
func NewServer(node *peer.Node) *Server {
	return &Server{node: node}
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

// SubscribeEvents streams incoming peer messages to the client.
func (s *Server) SubscribeEvents(req *clientrpcpb.SubscribeEventsRequest, stream grpc.ServerStreamingServer[clientrpcpb.Event]) error {
	ch := make(chan *clientrpcpb.Event, 64)

	s.node.Handle("chat", func(msg peer.Message) {
		ch <- &clientrpcpb.Event{
			Event: &clientrpcpb.Event_PeerMessage{
				PeerMessage: &clientrpcpb.PeerMessageEvent{
					From:        msg.From,
					MessageType: msg.Type,
					Payload:     msg.Payload,
				},
			},
		}
	})

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev := <-ch:
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// Run starts the gRPC server on the given address.
func Run(ctx context.Context, addr string, node *peer.Node) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := grpc.NewServer()
	clientrpcpb.RegisterPokerNodeServer(srv, NewServer(node))

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	return srv.Serve(lis)
}
