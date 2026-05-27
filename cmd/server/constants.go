package main

const (
	envCryptoKey          = "MVDL_CRYKEY"
	envAddr               = "ADDR"
	envPageSize           = "PAGE_SIZE"
	envUpstreamTimeout    = "UPSTREAM_TIMEOUT"
	envKnabenAPIURL       = "KNABEN_API_URL"
	envTorrentClawAPIKey  = "TORRENTCLAW_API_KEY"
	envTorrentClawAPIURL  = "TORRENTCLAW_API_URL"
	defaultKnabenAPIURL   = "https://api.knaben.org/v1"
	defaultTorrentClawURL = "https://torrentclaw.com/api/v1"

	FlagPageSize      = "page-size"
	FlagListen        = "listen"
	FlagProvider      = "provider"
	FlagTimeout       = "timeout"
	FlagInput         = "input"
	FlagStdin         = "stdin"
	FlagSaveTo        = "save-to"
	FlagTorrentListen = "torrent-listen"

	DefaultListenAddr        = "127.0.0.1:6567"
	DefaultGetAddr           = "127.0.0.1:6570"
	DefaultTorrentListenAddr = ":42069"

	SubCmdGet    = "get"
	SubCmdQuery  = "query"
	SubCmdServer = "server"
)
