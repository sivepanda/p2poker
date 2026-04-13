package protocol

const (
	KindRegisterReq       = "register_req"
	KindRegisterResp      = "register_resp"
	KindCreateSessionReq  = "create_session_req"
	KindCreateSessionResp = "create_session_resp"
	KindJoinSessionReq    = "join_session_req"
	KindJoinSessionResp   = "join_session_resp"
	KindListPeersReq      = "list_peers_req"
	KindListPeersResp     = "list_peers_resp"
	KindListSessionsReq   = "list_sessions_req"
	KindListSessionsResp  = "list_sessions_resp"
	KindHeartbeatReq      = "heartbeat_req"
	KindHeartbeatResp     = "heartbeat_resp"
	KindSetupPk           = "setup_pk"
	KindShufflePass       = "shuffle_pass"
	KindDealPass          = "deal_pass"
	KindCommit            = "commit"
	KindProposal          = "proposal"
	KindVerifyAck         = "verify_ack"
	KindAbort             = "abort"
	KindGameStart         = "game_start"
)

type Frame struct {
	Kind             string
	RequestID        string
	NodeID           string
	SessionID        string
	PeerAddr         string
	MessageType      string
	Payload          []byte
	Success          bool
	Error            string
	PeerIDs          []string
	PeerAddresses    []string
	SessionIDs       []string
	LeaseExpiresUnix int64
}
