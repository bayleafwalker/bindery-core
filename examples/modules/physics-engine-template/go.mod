module github.com/anvil-platform/anvil/examples/modules/physics-engine-template

go 1.22.0

require (
	github.com/anvil-platform/anvil v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats.go v1.39.1
	google.golang.org/grpc v1.65.0
)

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/nats-io/nkeys v0.4.9 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/protobuf v1.36.1 // indirect
)

replace github.com/anvil-platform/anvil => ../../..
