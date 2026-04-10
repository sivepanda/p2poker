package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sivepanda/p2poker/internal/protocol"
	"github.com/sivepanda/p2poker/internal/transport"
)

type Config struct {
	Address  string
	LeaseTTL time.Duration
}

type Server struct {
	cfg Config

	mu       sync.RWMutex
	sessions map[string]*session
	nodes    map[string]*nodeConn

	nodeCounter    uint64
	sessionCounter uint64
}

type session struct {
	id      string
	members map[string]struct{}
}

type nodeConn struct {
	id         string
	peerAddr   string
	sessionID  string
	leaseUntil time.Time
	conn       *transport.GobConn
	closed     bool
}

// NewServer validates config and initializes dispatch state.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Address == "" {
		return nil, errors.New("address must be set")
	}
	if cfg.LeaseTTL <= 0 {
		return nil, errors.New("lease ttl must be positive")
	}

	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*session),
		nodes:    make(map[string]*nodeConn),
	}, nil
}

// Run listens on the configured address until the context ends.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	s.serve(ctx, ln)
	return nil
}

// GetSessions reports every active session ID.
func (s *Server) GetSessions(ctx context.Context) []string {
	var sessionMap []string

	for _, value := range s.sessions {
		sessionMap = append(sessionMap, value.id)
	}
	return sessionMap

}

// GetUsersInSession lists the members of a session.
func (s *Server) GetUsersInSession(id string) map[string]struct{} {
	return s.sessions[id].members
}

// ListenAndServe starts serving in background and returns the bound address.
func (s *Server) ListenAndServe(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	go s.serve(ctx, ln)
	return ln.Addr().String(), nil
}

func (s *Server) serve(ctx context.Context, ln net.Listener) {
	go s.leaseReaper(ctx)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		go s.handleConn(ctx, transport.NewGobConn(conn))
	}
}

func (s *Server) handleConn(_ context.Context, conn *transport.GobConn) {
	nc, err := s.register(conn)
	if err != nil {
		_ = conn.Close()
		return
	}

	defer s.disconnect(nc.id)

	for {
		frame, err := conn.Receive()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = conn.Close()
			}
			return
		}

		s.handleFrame(nc, frame)
	}
}

func (s *Server) register(conn *transport.GobConn) (*nodeConn, error) {
	frame, err := conn.Receive()
	if err != nil {
		return nil, err
	}
	if frame.Kind != protocol.KindRegisterReq {
		_ = conn.Send(protocol.Frame{Kind: protocol.KindRegisterResp, Success: false, Error: "first frame must be register_req"})
		return nil, errors.New("invalid initial frame")
	}

	nodeID := frame.NodeID
	if nodeID == "" {
		nodeID = s.nextNodeID()
	}

	now := time.Now()
	nc := &nodeConn{
		id:         nodeID,
		peerAddr:   frame.PeerAddr,
		leaseUntil: now.Add(s.cfg.LeaseTTL),
		conn:       conn,
	}

	s.mu.Lock()
	if _, exists := s.nodes[nodeID]; exists {
		s.mu.Unlock()
		_ = conn.Send(protocol.Frame{Kind: protocol.KindRegisterResp, Success: false, Error: "node id already connected"})
		return nil, errors.New("duplicate node id")
	}
	s.nodes[nodeID] = nc
	s.mu.Unlock()

	_ = conn.Send(protocol.Frame{
		Kind:             protocol.KindRegisterResp,
		Success:          true,
		NodeID:           nodeID,
		LeaseExpiresUnix: nc.leaseUntil.Unix(),
	})

	return nc, nil
}

func (s *Server) handleFrame(nc *nodeConn, frame protocol.Frame) {
	s.renewLease(nc.id)

	switch frame.Kind {
	case protocol.KindCreateSessionReq:
		s.handleCreateSession(nc, frame)
	case protocol.KindJoinSessionReq:
		s.handleJoinSession(nc, frame)
	case protocol.KindListPeersReq:
		s.handleListPeers(nc, frame)
	case protocol.KindListSessionsReq:
		s.handleListSessions(nc, frame)
	case protocol.KindHeartbeatReq:
		s.handleHeartbeat(nc, frame)
	default:
		_ = nc.conn.Send(protocol.Frame{Kind: frame.Kind, RequestID: frame.RequestID, Success: false, Error: "unknown frame kind"})
	}
}

func (s *Server) handleCreateSession(nc *nodeConn, frame protocol.Frame) {
	sessionID := frame.SessionID
	if sessionID == "" {
		sessionID = s.nextSessionID()
	}

	s.mu.Lock()
	if _, exists := s.sessions[sessionID]; exists {
		s.mu.Unlock()
		_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindCreateSessionResp, RequestID: frame.RequestID, Success: false, Error: "session already exists"})
		return
	}

	if nc.sessionID != "" {
		s.removeNodeFromSessionLocked(nc.id, nc.sessionID)
	}

	sess := &session{id: sessionID, members: map[string]struct{}{nc.id: {}}}
	s.sessions[sessionID] = sess
	nc.sessionID = sessionID
	s.mu.Unlock()

	_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindCreateSessionResp, RequestID: frame.RequestID, Success: true, SessionID: sessionID})
}

func (s *Server) handleJoinSession(nc *nodeConn, frame protocol.Frame) {
	if frame.SessionID == "" {
		_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindJoinSessionResp, RequestID: frame.RequestID, Success: false, Error: "session id required"})
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[frame.SessionID]
	if !ok {
		s.mu.Unlock()
		_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindJoinSessionResp, RequestID: frame.RequestID, Success: false, Error: "session not found"})
		return
	}

	if nc.sessionID != "" {
		s.removeNodeFromSessionLocked(nc.id, nc.sessionID)
	}

	sess.members[nc.id] = struct{}{}
	nc.sessionID = frame.SessionID
	s.mu.Unlock()

	_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindJoinSessionResp, RequestID: frame.RequestID, Success: true, SessionID: frame.SessionID})
}

func (s *Server) handleListPeers(nc *nodeConn, frame protocol.Frame) {
	s.mu.RLock()
	if nc.sessionID == "" {
		s.mu.RUnlock()
		_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindListPeersResp, RequestID: frame.RequestID, Success: false, Error: "node is not in session"})
		return
	}

	sess := s.sessions[nc.sessionID]
	peerIDs := make([]string, 0, len(sess.members))
	peerAddrs := make([]string, 0, len(sess.members))
	for id := range sess.members {
		peer, ok := s.nodes[id]
		if !ok {
			continue
		}
		peerIDs = append(peerIDs, id)
		peerAddrs = append(peerAddrs, peer.peerAddr)
	}
	s.mu.RUnlock()

	_ = nc.conn.Send(protocol.Frame{
		Kind:          protocol.KindListPeersResp,
		RequestID:     frame.RequestID,
		Success:       true,
		SessionID:     nc.sessionID,
		PeerIDs:       peerIDs,
		PeerAddresses: peerAddrs,
	})
}

func (s *Server) handleListSessions(nc *nodeConn, frame protocol.Frame) {
	s.mu.RLock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	_ = nc.conn.Send(protocol.Frame{
		Kind:       protocol.KindListSessionsResp,
		RequestID:  frame.RequestID,
		Success:    true,
		SessionIDs: ids,
	})
}

func (s *Server) handleHeartbeat(nc *nodeConn, frame protocol.Frame) {
	lease := s.renewLease(nc.id)
	_ = nc.conn.Send(protocol.Frame{Kind: protocol.KindHeartbeatResp, RequestID: frame.RequestID, Success: true, LeaseExpiresUnix: lease.Unix()})
}

func (s *Server) renewLease(nodeID string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	nc, ok := s.nodes[nodeID]
	if !ok {
		return time.Time{}
	}

	nc.leaseUntil = time.Now().Add(s.cfg.LeaseTTL)
	return nc.leaseUntil
}

func (s *Server) leaseReaper(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			var expired []string

			s.mu.RLock()
			for id, node := range s.nodes {
				if now.After(node.leaseUntil) {
					expired = append(expired, id)
				}
			}
			s.mu.RUnlock()

			for _, id := range expired {
				s.disconnect(id)
			}
		}
	}
}

func (s *Server) disconnect(nodeID string) {
	s.mu.Lock()
	nc, ok := s.nodes[nodeID]
	if !ok {
		s.mu.Unlock()
		return
	}

	delete(s.nodes, nodeID)
	if nc.sessionID != "" {
		s.removeNodeFromSessionLocked(nodeID, nc.sessionID)
	}
	nc.closed = true
	s.mu.Unlock()

	_ = nc.conn.Close()
}

func (s *Server) removeNodeFromSessionLocked(nodeID, sessionID string) {
	sess, ok := s.sessions[sessionID]
	if !ok {
		return
	}

	delete(sess.members, nodeID)
	if len(sess.members) == 0 {
		delete(s.sessions, sessionID)
	}
}

func (s *Server) nextNodeID() string {
	v := atomic.AddUint64(&s.nodeCounter, 1)
	return fmt.Sprintf("node-%d", v)
}

func (s *Server) nextSessionID() string {
	v := atomic.AddUint64(&s.sessionCounter, 1)
	return fmt.Sprintf("session-%d", v)
}
