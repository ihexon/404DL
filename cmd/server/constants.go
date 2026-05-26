package main

const (
	MVDL_CRYKEY = "MVDL_CRYKEY"

	EnvTorrentClawAPIKey = "TORRENTCLAW_API_KEY"

	FlagPageSize             = "page-size"
	FlagListen               = "listen"
	FlagProvider             = "provider"
	FlagResolution           = "resolution"
	FlagTimeout              = "timeout"
	FlagSaveTo               = "save-to"
	FlagConnections          = "connections"
	FlagHalfOpen             = "half-open"
	FlagTotalHalfOpen        = "total-half-open"
	FlagPeerHighWater        = "peer-high-water"
	FlagPeerLowWater         = "peer-low-water"
	FlagDialRate             = "dial-rate"
	FlagMaxUnverifiedMiB     = "max-unverified-mib"
	FlagPeerRequestBufferMiB = "peer-request-buffer-mib"
	FlagPieceHashers         = "piece-hashers"
	FlagTorrentListen        = "torrent-listen"
	FlagNoUpload             = "no-upload"
	FlagSeed                 = "seed"
	FlagDisableIPv6          = "disable-ipv6"
	FlagDownloadRateMiB      = "download-rate-mib"
	FlagUploadRateMiB        = "upload-rate-mib"
	FlagProgressInterval     = "progress-interval"
	FlagStatusListen         = "status-listen"

	DefaultListenAddr = "127.0.0.1:6567"

	SubCmdDownload = "download"
	SubCmdQuery    = "query"
	SubCmdServer   = "server"

	KNABEN_API_URL      = "https://api.knaben.org/v1"
	TORRENTCLAW_API_URL = "https://torrentclaw.com/api/v1"
)
