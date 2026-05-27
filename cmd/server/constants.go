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

	FlagPageSize = "page-size"
	FlagListen   = "listen"
	FlagProvider = "provider"
	FlagFilter   = "filter"
	FlagTimeout  = "timeout"

	DefaultListenAddr = "127.0.0.1:6567"

	SubCmdQuery  = "query"
	SubCmdServer = "server"
)
