module github.com/Next-Lab-AI/next-trainer-enterprise/agent

go 1.22

require github.com/Next-Lab-AI/next-trainer-enterprise/protocol v0.0.0

require github.com/coder/websocket v1.8.12

require github.com/NVIDIA/go-nvml v0.13.4-1

require (
	github.com/minio/minio-go/v7 v7.0.88
	github.com/pelletier/go-toml/v2 v2.4.3
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/minio/crc64nvme v1.0.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/yeka/zip v0.0.0-20231116150916-03d6312748a9 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
)

replace github.com/Next-Lab-AI/next-trainer-enterprise/protocol => ../pkg/protocol
