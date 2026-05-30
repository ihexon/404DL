package get

import (
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dht "github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

const maxRuntimeEvents = 160

type RuntimeSnapshot struct {
	ID        string                `json:"id"`
	Updated   string                `json:"updated"`
	Summary   RuntimeSummary        `json:"summary"`
	Peers     []RuntimePeer         `json:"peers"`
	PieceRuns []RuntimePieceRun     `json:"pieceRuns"`
	DHT       []RuntimeDHTServer    `json:"dht"`
	Events    []RuntimeTorrentEvent `json:"events"`
}

type RuntimeSummary struct {
	InfoHash            string          `json:"infoHash,omitempty"`
	Name                string          `json:"name,omitempty"`
	MetadataReady       bool            `json:"metadataReady"`
	BytesCompleted      int64           `json:"bytesCompleted"`
	Length              int64           `json:"length"`
	Transfer            RuntimeTransfer `json:"transfer"`
	PendingPeers        int             `json:"pendingPeers"`
	ActivePeers         int             `json:"activePeers"`
	ConnectedSeeders    int             `json:"connectedSeeders"`
	HalfOpenPeers       int             `json:"halfOpenPeers"`
	PiecesComplete      int             `json:"piecesComplete"`
	NumPieces           int             `json:"numPieces"`
	ChunksReadUseful    int64           `json:"chunksReadUseful"`
	ChunksReadWasted    int64           `json:"chunksReadWasted"`
	BytesReadUsefulData int64           `json:"bytesReadUsefulData"`
	BytesWrittenData    int64           `json:"bytesWrittenData"`
}

type RuntimePeer struct {
	Address   string          `json:"address"`
	Source    string          `json:"source"`
	Network   string          `json:"network,omitempty"`
	Client    string          `json:"client,omitempty"`
	Transfer  RuntimeTransfer `json:"transfer"`
	Connected bool            `json:"connected"`
}

type RuntimeTransfer struct {
	DownloadRate float64 `json:"downloadRate"`
	UploadRate   float64 `json:"uploadRate"`
}

type RuntimePieceRun struct {
	Length     int    `json:"length"`
	State      string `json:"state"`
	Complete   bool   `json:"complete"`
	Partial    bool   `json:"partial"`
	Hashing    bool   `json:"hashing"`
	QueuedHash bool   `json:"queuedHash"`
	Priority   string `json:"priority"`
}

type RuntimeDHTServer struct {
	ID                              string `json:"id"`
	Address                         string `json:"address"`
	Nodes                           int    `json:"nodes"`
	GoodNodes                       int    `json:"goodNodes"`
	OutstandingTransactions         int    `json:"outstandingTransactions"`
	OutboundQueriesAttempted        int64  `json:"outboundQueriesAttempted"`
	SuccessfulOutboundAnnouncePeers int64  `json:"successfulOutboundAnnouncePeers"`
	BadNodes                        uint   `json:"badNodes"`
}

type RuntimeTorrentEvent struct {
	Time     string `json:"time"`
	Type     string `json:"type"`
	InfoHash string `json:"infoHash,omitempty"`
	Peer     string `json:"peer,omitempty"`
	Source   string `json:"source,omitempty"`
	Network  string `json:"network,omitempty"`
	Client   string `json:"client,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	DHTQuery string `json:"dhtQuery,omitempty"`
	DHTNode  string `json:"dhtNode,omitempty"`
}

type runtimeCollector struct {
	mu     sync.Mutex
	events map[string][]RuntimeTorrentEvent
}

func newRuntimeCollector() *runtimeCollector {
	return &runtimeCollector{
		events: make(map[string][]RuntimeTorrentEvent),
	}
}

func (c *runtimeCollector) callbacks() torrent.Callbacks {
	return torrent.Callbacks{
		CompletedHandshake:    c.completedHandshake,
		ReadExtendedHandshake: c.readExtendedHandshake,
		PeerConnClosed:        c.peerConnClosed,
		PeerConnAdded: []func(*torrent.PeerConn){
			c.peerConnAdded,
		},
		StatusUpdated: []func(torrent.StatusUpdatedEvent){
			c.statusUpdated,
		},
	}
}

func (c *runtimeCollector) snapshot(id string, client *torrent.Client, t *torrent.Torrent) RuntimeSnapshot {
	peers := activePeers(t)
	snapshot := RuntimeSnapshot{
		ID:        id,
		Updated:   time.Now().Format(time.RFC3339),
		Summary:   torrentSummary(t, peers),
		Peers:     torrentPeers(peers),
		PieceRuns: c.torrentPieceRuns(t),
		DHT:       dhtServers(client),
		Events:    c.eventsFor(t.InfoHash().HexString()),
	}
	snapshot.normalize()
	return snapshot
}

func (s *RuntimeSnapshot) normalize() {
	if s.Peers == nil {
		s.Peers = []RuntimePeer{}
	}
	if s.PieceRuns == nil {
		s.PieceRuns = []RuntimePieceRun{}
	}
	if s.DHT == nil {
		s.DHT = []RuntimeDHTServer{}
	}
	if s.Events == nil {
		s.Events = []RuntimeTorrentEvent{}
	}
}

func (c *runtimeCollector) completedHandshake(pc *torrent.PeerConn, infoHash torrent.InfoHash) {
	if pc == nil {
		return
	}
	c.append(infoHash.HexString(), peerEvent("handshake", &pc.Peer))
}

func (c *runtimeCollector) readExtendedHandshake(pc *torrent.PeerConn, msg *pp.ExtendedHandshakeMessage) {
	if pc == nil || msg == nil {
		return
	}
	event := peerEvent("extended_handshake", &pc.Peer)
	event.Client = strings.TrimSpace(msg.V)
	c.append(peerConnInfoHash(pc), event)
}

func (c *runtimeCollector) peerConnAdded(pc *torrent.PeerConn) {
	if pc == nil {
		return
	}
	c.append(peerConnInfoHash(pc), peerEvent("peer_connected", &pc.Peer))
}

func (c *runtimeCollector) peerConnClosed(pc *torrent.PeerConn) {
	if pc == nil {
		return
	}
	c.append(peerConnInfoHash(pc), peerEvent("peer_closed", &pc.Peer))
}

func (c *runtimeCollector) statusUpdated(event torrent.StatusUpdatedEvent) {
	infoHash := event.InfoHash
	if infoHash == "" {
		return
	}
	runtimeEvent := RuntimeTorrentEvent{
		Time:     time.Now().Format(time.RFC3339),
		Type:     string(event.Event),
		InfoHash: infoHash,
		Peer:     event.PeerId.String(),
		Message:  event.Url,
	}
	if event.Error != nil {
		runtimeEvent.Error = event.Error.Error()
	}
	c.append(infoHash, runtimeEvent)
}

func (c *runtimeCollector) observeDHTQuery(query *krpc.Msg, source string) {
	if query == nil || query.A == nil || query.A.InfoHash.IsZero() {
		return
	}
	c.append(query.A.InfoHash.String(), RuntimeTorrentEvent{
		Time:     time.Now().Format(time.RFC3339),
		Type:     "dht_query",
		InfoHash: query.A.InfoHash.String(),
		DHTQuery: query.Q,
		DHTNode:  source,
	})
}

func (c *runtimeCollector) eventsFor(infoHash string) []RuntimeTorrentEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	events := c.events[infoHash]
	out := make([]RuntimeTorrentEvent, len(events))
	copy(out, events)
	return out
}

func (c *runtimeCollector) append(infoHash string, event RuntimeTorrentEvent) {
	if infoHash == "" {
		return
	}
	if event.Time == "" {
		event.Time = time.Now().Format(time.RFC3339)
	}
	event.InfoHash = infoHash

	c.mu.Lock()
	defer c.mu.Unlock()

	events := append(c.events[infoHash], event)
	if len(events) > maxRuntimeEvents {
		events = append([]RuntimeTorrentEvent(nil), events[len(events)-maxRuntimeEvents:]...)
	}
	c.events[infoHash] = events
}

func peerEvent(eventType string, peer *torrent.Peer) RuntimeTorrentEvent {
	event := RuntimeTorrentEvent{
		Time:    time.Now().Format(time.RFC3339),
		Type:    eventType,
		Peer:    peerAddress(peer),
		Source:  peerSource(peer),
		Network: peerNetwork(peer),
	}
	if peer == nil {
		return event
	}
	if pc, ok := peer.TryAsPeerConn(); ok {
		if clientName, ok := pc.PeerClientName.Load().(string); ok {
			event.Client = strings.TrimSpace(clientName)
		}
	}
	return event
}

func torrentSummary(t *torrent.Torrent, peers []RuntimePeer) RuntimeSummary {
	summary := RuntimeSummary{
		InfoHash: t.InfoHash().HexString(),
		Name:     t.Name(),
		Transfer: totalTransfer(peers),
	}
	if t.Info() != nil {
		summary.MetadataReady = true
		stats := t.Stats()
		summary.PendingPeers = stats.PendingPeers
		summary.ActivePeers = stats.ActivePeers
		summary.ConnectedSeeders = stats.ConnectedSeeders
		summary.HalfOpenPeers = stats.HalfOpenPeers
		summary.PiecesComplete = stats.PiecesComplete
		summary.ChunksReadUseful = stats.ChunksReadUseful.Int64()
		summary.ChunksReadWasted = stats.ChunksReadWasted.Int64()
		summary.BytesReadUsefulData = stats.BytesReadUsefulData.Int64()
		summary.BytesWrittenData = stats.BytesWrittenData.Int64()
		summary.BytesCompleted = t.BytesCompleted()
		summary.Length = t.Length()
		summary.NumPieces = int(t.NumPieces())
	}
	return summary
}

func totalTransfer(peers []RuntimePeer) RuntimeTransfer {
	var total RuntimeTransfer
	for _, peer := range peers {
		total.DownloadRate += peer.Transfer.DownloadRate
		total.UploadRate += peer.Transfer.UploadRate
	}
	return total
}

func torrentPeers(peers []RuntimePeer) []RuntimePeer {
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].Transfer.DownloadRate != peers[j].Transfer.DownloadRate {
			return peers[i].Transfer.DownloadRate > peers[j].Transfer.DownloadRate
		}
		if peers[i].Transfer.UploadRate != peers[j].Transfer.UploadRate {
			return peers[i].Transfer.UploadRate > peers[j].Transfer.UploadRate
		}
		return peers[i].Address < peers[j].Address
	})
	if len(peers) > 30 {
		return peers[:30]
	}
	return peers
}

func activePeers(t *torrent.Torrent) []RuntimePeer {
	conns := t.PeerConns()
	peers := make([]RuntimePeer, 0, len(conns))
	for _, conn := range conns {
		stats := conn.Stats()
		peer := RuntimePeer{
			Address: peerAddress(&conn.Peer),
			Source:  peerSource(&conn.Peer),
			Network: peerNetwork(&conn.Peer),
			Transfer: RuntimeTransfer{
				DownloadRate: stats.DownloadRate,
				UploadRate:   stats.LastWriteUploadRate,
			},
			Connected: true,
		}
		if clientName, ok := conn.PeerClientName.Load().(string); ok {
			peer.Client = strings.TrimSpace(clientName)
		}
		peers = append(peers, peer)
	}
	return peers
}

func (c *runtimeCollector) torrentPieceRuns(t *torrent.Torrent) []RuntimePieceRun {
	if t.Info() == nil {
		return nil
	}
	runs := t.PieceStateRuns()
	out := make([]RuntimePieceRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, RuntimePieceRun{
			Length:     run.Length,
			State:      pieceRunState(run),
			Complete:   run.Complete,
			Partial:    run.Partial,
			Hashing:    run.Hashing,
			QueuedHash: run.QueuedForHash,
			Priority:   piecePriority(run.Priority),
		})
	}
	return out
}

func dhtServers(client *torrent.Client) []RuntimeDHTServer {
	servers := client.DhtServers()
	out := make([]RuntimeDHTServer, 0, len(servers))
	for _, server := range servers {
		if server == nil {
			continue
		}
		out = append(out, dhtServer(server))
	}
	return out
}

func dhtServer(server torrent.DhtServer) RuntimeDHTServer {
	stats, _ := server.Stats().(dht.ServerStats)
	id := server.ID()
	return RuntimeDHTServer{
		ID:                              hex.EncodeToString(id[:]),
		Address:                         addrString(server.Addr()),
		Nodes:                           stats.Nodes,
		GoodNodes:                       stats.GoodNodes,
		OutstandingTransactions:         stats.OutstandingTransactions,
		OutboundQueriesAttempted:        stats.OutboundQueriesAttempted,
		SuccessfulOutboundAnnouncePeers: stats.SuccessfulOutboundAnnouncePeerQueries,
		BadNodes:                        stats.BadNodes,
	}
}

func pieceRunState(run torrent.PieceStateRun) string {
	switch {
	case run.Complete:
		return "complete"
	case run.Hashing:
		return "hashing"
	case run.QueuedForHash:
		return "queued_hash"
	case run.Partial:
		return "partial"
	default:
		return "empty"
	}
}

func piecePriority(priority torrent.PiecePriority) string {
	switch priority {
	case 0:
		return "none"
	case 1:
		return "normal"
	case 2:
		return "high"
	case 3:
		return "readahead"
	case 4:
		return "next"
	case 5:
		return "now"
	default:
		return strconv.Itoa(int(priority))
	}
}

func peerAddress(peer *torrent.Peer) string {
	if peer == nil {
		return ""
	}
	return addrString(peer.RemoteAddr)
}

func peerSource(peer *torrent.Peer) string {
	if peer == nil {
		return ""
	}
	return string(peer.Discovery)
}

func peerNetwork(peer *torrent.Peer) string {
	if peer == nil {
		return ""
	}
	return peer.Network
}

func peerConnInfoHash(pc *torrent.PeerConn) string {
	if pc == nil || pc.Torrent() == nil {
		return ""
	}
	return pc.Torrent().InfoHash().HexString()
}

func addrString(addr interface{ String() string }) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}
