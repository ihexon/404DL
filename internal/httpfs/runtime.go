package httpfs

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dht "github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	pp "github.com/anacrolix/torrent/peer_protocol"
	"github.com/sirupsen/logrus"
)

const maxRuntimeEvents = 160

type RuntimeSnapshot struct {
	ID      string                `json:"id"`
	Updated string                `json:"updated"`
	Summary RuntimeSummary        `json:"summary"`
	Peers   []RuntimePeer         `json:"peers"`
	Pieces  []RuntimePieceRun     `json:"pieces"`
	DHT     []RuntimeDHTServer    `json:"dht"`
	Events  []RuntimeTorrentEvent `json:"events"`
}

type RuntimeSummary struct {
	InfoHash               string `json:"infoHash,omitempty"`
	Name                   string `json:"name,omitempty"`
	MetadataReady          bool   `json:"metadataReady"`
	BytesCompleted         int64  `json:"bytesCompleted"`
	BytesMissing           int64  `json:"bytesMissing"`
	Length                 int64  `json:"length"`
	TotalPeers             int    `json:"totalPeers"`
	PendingPeers           int    `json:"pendingPeers"`
	ActivePeers            int    `json:"activePeers"`
	ConnectedSeeders       int    `json:"connectedSeeders"`
	HalfOpenPeers          int    `json:"halfOpenPeers"`
	PiecesComplete         int    `json:"piecesComplete"`
	NumPieces              int    `json:"numPieces"`
	ChunksReadUseful       int64  `json:"chunksReadUseful"`
	ChunksReadWasted       int64  `json:"chunksReadWasted"`
	BytesReadData          int64  `json:"bytesReadData"`
	BytesReadUsefulData    int64  `json:"bytesReadUsefulData"`
	BytesWrittenData       int64  `json:"bytesWrittenData"`
	KnownPeers             int    `json:"knownPeers"`
	ActiveHalfOpenAttempts int    `json:"activeHalfOpenAttempts"`
}

type RuntimePeer struct {
	Address             string  `json:"address"`
	Source              string  `json:"source"`
	Network             string  `json:"network,omitempty"`
	Client              string  `json:"client,omitempty"`
	PeerID              string  `json:"peerId,omitempty"`
	DownloadRate        float64 `json:"downloadRate"`
	UploadRate          float64 `json:"uploadRate"`
	RemotePieceCount    int     `json:"remotePieceCount"`
	BytesReadData       int64   `json:"bytesReadData"`
	BytesReadUsefulData int64   `json:"bytesReadUsefulData"`
	BytesWrittenData    int64   `json:"bytesWrittenData"`
	ChunksReadUseful    int64   `json:"chunksReadUseful"`
	ChunksReadWasted    int64   `json:"chunksReadWasted"`
	Connected           bool    `json:"connected"`
	SupportsEncryption  bool    `json:"supportsEncryption"`
}

type RuntimePieceRun struct {
	Start      int    `json:"start"`
	End        int    `json:"end"`
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
	Piece    *int   `json:"piece,omitempty"`
	Begin    *int   `json:"begin,omitempty"`
	Length   *int   `json:"length,omitempty"`
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
		ReceivedUsefulData: []func(torrent.ReceivedUsefulDataEvent){
			c.receivedUsefulData,
		},
		ReceivedRequested: []func(torrent.PeerMessageEvent){
			c.receivedRequested,
		},
		SentRequest: []func(torrent.PeerRequestEvent){
			c.sentRequest,
		},
		StatusUpdated: []func(torrent.StatusUpdatedEvent){
			c.statusUpdated,
		},
	}
}

func (c *runtimeCollector) snapshot(id string, client *torrent.Client, t *torrent.Torrent) RuntimeSnapshot {
	knownSwarm := torrentKnownSwarm(t)
	snapshot := RuntimeSnapshot{
		ID:      id,
		Updated: time.Now().Format(time.RFC3339),
		Summary: torrentSummary(client, t, len(knownSwarm)),
		Peers:   torrentPeers(t, knownSwarm),
		Pieces:  torrentPieceRuns(t),
		DHT:     dhtServers(client),
		Events:  c.eventsFor(t.InfoHash().HexString()),
	}
	return snapshot
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

func (c *runtimeCollector) receivedUsefulData(event torrent.ReceivedUsefulDataEvent) {
	peer := event.Peer
	if peer == nil || peer.Torrent() == nil || event.Message == nil {
		return
	}
	pe := peerEvent("chunk_received", peer)
	pe.Piece = intPtr(event.Message.Index.Int())
	pe.Begin = intPtr(event.Message.Begin.Int())
	pe.Length = intPtr(len(event.Message.Piece))
	c.append(peer.Torrent().InfoHash().HexString(), pe)
}

func (c *runtimeCollector) receivedRequested(event torrent.PeerMessageEvent) {
	peer := event.Peer
	if peer == nil || peer.Torrent() == nil || event.Message == nil {
		return
	}
	pe := peerEvent("requested_received", peer)
	pe.Piece = intPtr(event.Message.Index.Int())
	pe.Begin = intPtr(event.Message.Begin.Int())
	pe.Length = intPtr(event.Message.Length.Int())
	pe.Message = event.Message.Type.String()
	c.append(peer.Torrent().InfoHash().HexString(), pe)
}

func (c *runtimeCollector) sentRequest(event torrent.PeerRequestEvent) {
	peer := event.Peer
	if peer == nil || peer.Torrent() == nil {
		return
	}
	pe := peerEvent("request_sent", peer)
	pe.Piece = intPtr(event.Index.Int())
	pe.Begin = intPtr(event.Begin.Int())
	pe.Length = intPtr(event.Length.Int())
	c.append(peer.Torrent().InfoHash().HexString(), pe)
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

func torrentSummary(client *torrent.Client, t *torrent.Torrent, knownPeers int) RuntimeSummary {
	clientStats := client.Stats()
	summary := RuntimeSummary{
		InfoHash:               t.InfoHash().HexString(),
		Name:                   t.Name(),
		KnownPeers:             knownPeers,
		ActiveHalfOpenAttempts: clientStats.ActiveHalfOpenAttempts,
	}
	if t.Info() != nil {
		summary.MetadataReady = true
		stats := t.Stats()
		summary.TotalPeers = stats.TotalPeers
		summary.PendingPeers = stats.PendingPeers
		summary.ActivePeers = stats.ActivePeers
		summary.ConnectedSeeders = stats.ConnectedSeeders
		summary.HalfOpenPeers = stats.HalfOpenPeers
		summary.PiecesComplete = stats.PiecesComplete
		summary.ChunksReadUseful = stats.ChunksReadUseful.Int64()
		summary.ChunksReadWasted = stats.ChunksReadWasted.Int64()
		summary.BytesReadData = stats.BytesReadData.Int64()
		summary.BytesReadUsefulData = stats.BytesReadUsefulData.Int64()
		summary.BytesWrittenData = stats.BytesWrittenData.Int64()
		summary.BytesCompleted = t.BytesCompleted()
		summary.BytesMissing = t.BytesMissing()
		summary.Length = t.Length()
		summary.NumPieces = int(t.NumPieces())
	}
	return summary
}

func torrentKnownSwarm(t *torrent.Torrent) (peers []torrent.PeerInfo) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logrus.WithFields(logrus.Fields{
				"info_hash": t.InfoHash().HexString(),
				"panic":     fmt.Sprint(recovered),
			}).Warn("httpfs runtime known swarm skipped")
			peers = nil
		}
	}()
	return t.KnownSwarm()
}

func torrentPeers(t *torrent.Torrent, knownSwarm []torrent.PeerInfo) []RuntimePeer {
	active := activePeers(t)
	seen := make(map[string]struct{}, len(active))
	peers := make([]RuntimePeer, 0, len(active))
	for _, peer := range active {
		peers = append(peers, peer)
		seen[peer.Address] = struct{}{}
	}
	for _, peer := range knownSwarm {
		addr := addrString(peer.Addr)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		peers = append(peers, RuntimePeer{
			Address:            addr,
			Source:             string(peer.Source),
			Connected:          false,
			SupportsEncryption: peer.SupportsEncryption,
		})
	}
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].Connected != peers[j].Connected {
			return peers[i].Connected
		}
		if peers[i].DownloadRate != peers[j].DownloadRate {
			return peers[i].DownloadRate > peers[j].DownloadRate
		}
		return peers[i].Address < peers[j].Address
	})
	if len(peers) > 80 {
		return peers[:80]
	}
	return peers
}

func activePeers(t *torrent.Torrent) []RuntimePeer {
	conns := t.PeerConns()
	peers := make([]RuntimePeer, 0, len(conns))
	for _, conn := range conns {
		stats := conn.Stats()
		peer := RuntimePeer{
			Address:             peerAddress(&conn.Peer),
			Source:              peerSource(&conn.Peer),
			Network:             peerNetwork(&conn.Peer),
			PeerID:              conn.PeerID.String(),
			DownloadRate:        stats.DownloadRate,
			UploadRate:          stats.LastWriteUploadRate,
			RemotePieceCount:    stats.RemotePieceCount,
			BytesReadData:       stats.BytesReadData.Int64(),
			BytesReadUsefulData: stats.BytesReadUsefulData.Int64(),
			BytesWrittenData:    stats.BytesWrittenData.Int64(),
			ChunksReadUseful:    stats.ChunksReadUseful.Int64(),
			ChunksReadWasted:    stats.ChunksReadWasted.Int64(),
			Connected:           true,
			SupportsEncryption:  conn.PeerPrefersEncryption,
		}
		if clientName, ok := conn.PeerClientName.Load().(string); ok {
			peer.Client = strings.TrimSpace(clientName)
		}
		peers = append(peers, peer)
	}
	return peers
}

func torrentPieceRuns(t *torrent.Torrent) []RuntimePieceRun {
	if t.Info() == nil {
		return nil
	}
	runs := t.PieceStateRuns()
	out := make([]RuntimePieceRun, 0, len(runs))
	start := 0
	for _, run := range runs {
		end := start + run.Length - 1
		out = append(out, RuntimePieceRun{
			Start:      start,
			End:        end,
			Length:     run.Length,
			State:      pieceRunState(run),
			Complete:   run.Complete,
			Partial:    run.Partial,
			Hashing:    run.Hashing,
			QueuedHash: run.QueuedForHash,
			Priority:   piecePriority(run.Priority),
		})
		start += run.Length
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
	case run.Priority > 0:
		return "wanted"
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

func intPtr(value int) *int {
	return &value
}

func addrString(addr interface{ String() string }) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}
