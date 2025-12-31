package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
)

func main() {
	var target string
	var worldID string
	flag.StringVar(&target, "target", "127.0.0.1:50051", "gRPC server address")
	flag.StringVar(&worldID, "world", "world-1", "world id")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Errorf("dial %s: %w", target, err))
	}
	defer conn.Close()

	c := enginev1.NewEngineModuleClient(conn)

	resp, err := c.GetStateSnapshot(ctx, &enginev1.GetStateSnapshotRequest{
		WorldId:   worldID,
		RequestId: fmt.Sprintf("smoke-%d", time.Now().UnixNano()),
		Selector:  &enginev1.GetStateSnapshotRequest_Latest{Latest: &enginev1.SnapshotLatest{}},
		// Keeping the request minimal for a quick smoke test.
	})
	if err != nil {
		fmt.Printf("GetStateSnapshot error: %v\n", err)
		return
	}

	switch r := resp.GetResult().(type) {
	case *enginev1.GetStateSnapshotResponse_Ok:
		fmt.Printf("GetStateSnapshot ok: tick=%d entities=%d\n", r.Ok.GetWorldState().GetTick(), len(r.Ok.GetWorldState().GetEntities()))
	case *enginev1.GetStateSnapshotResponse_Error:
		fmt.Printf("GetStateSnapshot error result: code=%s message=%q\n", r.Error.GetCode().String(), r.Error.GetMessage())
	default:
		fmt.Printf("GetStateSnapshot unknown result\n")
	}
}
