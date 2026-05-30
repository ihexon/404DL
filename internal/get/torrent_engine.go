package get

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/anacrolix/generics"
	"golang.org/x/time/rate"

	dht "github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/sirupsen/logrus"
)

const (
	torrentEstablishedConnsPerTorrent     = 256
	torrentHalfOpenConnsPerTorrent        = 96
	torrentTotalHalfOpenConns             = 512
	torrentPeersHighWater                 = 2000
	torrentPeersLowWater                  = 500
	torrentDialRatePerSecond              = 500
	torrentDialRateBurst                  = 1000
	torrentMaxUnverifiedBytes             = 512 << 20
	torrentMaxAllocPeerRequestDataPerConn = 2 << 20
	torrentPieceHashersPerTorrent         = 8
)

type torrentEngine struct {
	dataDir string
	client  *torrent.Client
	runtime *runtimeCollector
}

func newTorrentEngine(dataDir, torrentListenAddr string) (*torrentEngine, error) {
	runtime := newRuntimeCollector()
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Callbacks = runtime.callbacks()
	cfg.ConfigureAnacrolixDhtServer = func(cfg *dht.ServerConfig) {
		onQuery := cfg.OnQuery
		cfg.OnQuery = func(query *krpc.Msg, source net.Addr) bool {
			runtime.observeDHTQuery(query, addrString(source))
			if onQuery != nil {
				return onQuery(query, source)
			}
			return true
		}
	}
	cfg.EstablishedConnsPerTorrent = torrentEstablishedConnsPerTorrent
	cfg.HalfOpenConnsPerTorrent = torrentHalfOpenConnsPerTorrent
	cfg.TotalHalfOpenConns = torrentTotalHalfOpenConns
	cfg.TorrentPeersHighWater = torrentPeersHighWater
	cfg.TorrentPeersLowWater = torrentPeersLowWater
	cfg.NominalDialTimeout = 10 * time.Second
	cfg.MinDialTimeout = 2 * time.Second
	cfg.HandshakesTimeout = 3 * time.Second
	cfg.KeepAliveTimeout = 45 * time.Second
	cfg.MaxAllocPeerRequestDataPerConn = torrentMaxAllocPeerRequestDataPerConn
	cfg.NoDHT = false
	cfg.DisableTrackers = false
	cfg.DisablePEX = false
	cfg.DisableWebseeds = false
	cfg.DisableWebtorrent = false
	cfg.NoDefaultPortForwarding = false
	cfg.DialForPeerConns = true
	cfg.AcceptPeerConnections = true
	cfg.AlwaysWantConns = true
	cfg.DisableAcceptRateLimiting = true
	cfg.PeriodicallyAnnounceTorrentsToDht = true
	cfg.LocalServiceDiscovery = &torrent.LocalServiceDiscoveryConfig{}
	cfg.Seed = true
	cfg.DialRateLimiter = rate.NewLimiter(torrentDialRatePerSecond, torrentDialRateBurst)
	cfg.MaxUnverifiedBytes = torrentMaxUnverifiedBytes
	cfg.PieceHashersPerTorrent = torrentPieceHashersPerTorrent
	if err := configureTorrentListenAddr(cfg, torrentListenAddr); err != nil {
		return nil, err
	}
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"data_dir":                dataDir,
		"torrent_listen":          torrentListenAddr,
		"torrent_peer_high":       cfg.TorrentPeersHighWater,
		"torrent_peer_low":        cfg.TorrentPeersLowWater,
		"established_per_torrent": cfg.EstablishedConnsPerTorrent,
		"half_open_per_torrent":   cfg.HalfOpenConnsPerTorrent,
		"total_half_open":         cfg.TotalHalfOpenConns,
		"dial_rate_per_second":    torrentDialRatePerSecond,
		"dial_rate_burst":         torrentDialRateBurst,
		"max_unverified_bytes":    cfg.MaxUnverifiedBytes,
		"max_peer_request_buffer": cfg.MaxAllocPeerRequestDataPerConn,
		"piece_hashers":           cfg.PieceHashersPerTorrent,
		"local_peer_discovery":    cfg.LocalServiceDiscovery != nil,
		"dht":                     !cfg.NoDHT,
		"trackers":                !cfg.DisableTrackers,
		"pex":                     !cfg.DisablePEX,
		"webseeds":                !cfg.DisableWebseeds,
		"seed":                    cfg.Seed,
		"part_files":              true,
	}).Info("torrent engine initialized")
	return &torrentEngine{
		dataDir: dataDir,
		client:  client,
		runtime: runtime,
	}, nil
}

func configureTorrentListenAddr(cfg *torrent.ClientConfig, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = defaultTorrentListenAddr
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse torrent listen address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("parse torrent listen port %q: %w", portText, err)
	}
	cfg.ListenHost = func(string) string { return host }
	cfg.ListenPort = port
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			cfg.DisableIPv6 = true
		} else {
			cfg.DisableIPv4 = true
		}
	}
	return nil
}

func torrentStorageFilePath(info *metainfo.Info, file metainfo.FileInfo) string {
	var parts []string
	if info.BestName() != metainfo.NoName {
		parts = append(parts, info.BestName())
	}
	parts = append(parts, file.BestPath()...)
	return filepath.Join(parts...)
}

func (e *torrentEngine) Close() error {
	e.client.Close()
	return nil
}

func (e *torrentEngine) loadedTorrent(hash string) (*torrent.Torrent, bool, error) {
	if hash == "" {
		return nil, true, nil
	}
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, true, fmt.Errorf("parse info hash %q: %w", hash, err)
	}
	t, ok := e.client.Torrent(ih)
	if !ok {
		return nil, true, nil
	}
	return t, true, nil
}

func (e *torrentEngine) addTorrent(hash, magnetURL, path string, infoBytes []byte) (*torrent.Torrent, error) {
	if len(infoBytes) > 0 {
		spec, err := e.torrentSpec(hash, magnetURL, path)
		if err != nil {
			return nil, err
		}
		spec.InfoBytes = infoBytes
		t, _, err := e.client.AddTorrentSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("add torrent metainfo: %w", err)
		}
		return t, nil
	}
	if magnetURL != "" {
		spec, err := e.torrentSpec(hash, magnetURL, path)
		if err != nil {
			return nil, err
		}
		t, _, err := e.client.AddTorrentSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("add magnet: %w", err)
		}
		return t, nil
	}
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, fmt.Errorf("parse info hash %q: %w", hash, err)
	}
	t, _ := e.client.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash:                        ih,
		Storage:                         e.taskStorage(path),
		IgnoreUnverifiedPieceCompletion: true,
	})
	return t, nil
}

func (e *torrentEngine) runtimeSnapshot(id string, t *torrent.Torrent) RuntimeSnapshot {
	return e.runtime.snapshot(id, e.client, t)
}

func (e *torrentEngine) torrentSpec(hash, magnetURL, path string) (*torrent.TorrentSpec, error) {
	if magnetURL != "" {
		spec, err := torrent.TorrentSpecFromMagnetUri(magnetURL)
		if err != nil {
			return nil, fmt.Errorf("parse magnet: %w", err)
		}
		spec.Storage = e.taskStorage(path)
		spec.IgnoreUnverifiedPieceCompletion = true
		return spec, nil
	}
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, fmt.Errorf("parse info hash %q: %w", hash, err)
	}
	return &torrent.TorrentSpec{
		AddTorrentOpts: torrent.AddTorrentOpts{
			InfoHash:                        ih,
			Storage:                         e.taskStorage(path),
			IgnoreUnverifiedPieceCompletion: true,
		},
	}, nil
}

func (e *torrentEngine) taskStorage(path string) storage.ClientImplCloser {
	return storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: e.dataDir,
		TorrentDirMaker: func(string, *metainfo.Info, metainfo.Hash) string {
			return path
		},
		UsePartFiles: g.Some(true),
	})
}
