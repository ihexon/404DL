package main

const (
	envTorrentClawAPIKey  = "TORRENTCLAW_API_KEY"
	defaultKnabenAPIURL   = "https://api.knaben.org/v1"
	defaultTorrentClawURL = "https://torrentclaw.com/api/v1"

	DefaultSearchLimit = 50

	FlagLimitSize     = "limit-size"
	FlagListen        = "listen"
	FlagStateDir      = "state-dir"
	FlagTimeout       = "timeout"
	FlagDownloadDir   = "download-dir"
	FlagTorrentListen = "torrent-listen"

	DefaultGetAddr           = "127.0.0.1:0"
	DefaultTorrentListenAddr = ":42069"
)
