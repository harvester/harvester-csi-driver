package csi

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// blockingIdentityServer holds every Probe open until release is closed, so a
// test can keep an RPC in flight across a shutdown.
type blockingIdentityServer struct {
	csi.UnimplementedIdentityServer
	started chan struct{}
	release chan struct{}
}

func (b *blockingIdentityServer) Probe(ctx context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	close(b.started)
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return &csi.ProbeResponse{}, nil
}

func startTestServer(t *testing.T, ids csi.IdentityServer) (*NonBlockingGRPCServer, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "csi.sock")
	s := NewNonBlockingGRPCServer()
	s.Start("unix://"+sock, ids, nil, nil)
	return s, sock
}

func waitReturns(t *testing.T, s *NonBlockingGRPCServer, within time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("Wait did not return within %s", within)
	}
}

func TestStopReleasesWait(t *testing.T) {
	s, _ := startTestServer(t, NewIdentityServer(driverName, "test"))
	s.Stop()
	waitReturns(t, s, 2*time.Second)
}

func TestStopOnDoneStopsWhenContextIsCancelled(t *testing.T) {
	s, _ := startTestServer(t, NewIdentityServer(driverName, "test"))
	ctx, cancel := context.WithCancel(context.Background())
	go s.StopOnDone(ctx, time.Second)
	cancel()
	waitReturns(t, s, 2*time.Second)
}

func TestStopOnDoneForcesStopAfterTimeout(t *testing.T) {
	ids := &blockingIdentityServer{started: make(chan struct{}), release: make(chan struct{})}
	defer close(ids.release)
	s, sock := startTestServer(t, ids)

	conn, err := grpc.NewClient("unix://"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		_, _ = csi.NewIdentityClient(conn).Probe(context.Background(), &csi.ProbeRequest{})
	}()
	select {
	case <-ids.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Probe never reached the server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go s.StopOnDone(ctx, 200*time.Millisecond)
	waitReturns(t, s, 2*time.Second)
}
