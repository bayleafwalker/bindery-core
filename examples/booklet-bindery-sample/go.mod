module github.com/bayleafwalker/bindery-sample-game

go 1.22.0

require (
	github.com/bayleafwalker/bindery-core v0.0.0
	google.golang.org/grpc v1.65.0
)

require (
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/bayleafwalker/bindery-core => ../..
