package main

const (
	envCryptoKey          = "FOURDL_CRYKEY"
	envTorrentClawAPIKey  = "TORRENTCLAW_API_KEY"
	defaultKnabenAPIURL   = "https://api.knaben.org/v1"
	defaultTorrentClawURL = "https://torrentclaw.com/api/v1"

	FlagLimitSize     = "limit-size"
	FlagListen        = "listen"
	FlagProvider      = "provider"
	FlagServerURL     = "server-url"
	FlagTimeout       = "timeout"
	FlagInput         = "input"
	FlagStdin         = "stdin"
	FlagSaveTo        = "save-to"
	FlagTorrentListen = "torrent-listen"

	DefaultListenAddr        = "127.0.0.1:6567"
	DefaultGetAddr           = "127.0.0.1:6570"
	DefaultTorrentListenAddr = ":42069"

	SubCmdGet    = "get"
	SubCmdSearch = "search"
	SubCmdServer = "server"
)
