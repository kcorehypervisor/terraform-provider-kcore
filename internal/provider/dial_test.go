package provider

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func TestDialTargetKeepsThePassthroughResolver(t *testing.T) {
	for addr, want := range map[string]string{
		"10.0.0.5:9090":         "passthrough:///10.0.0.5:9090",
		"controller.lan:9090":   "passthrough:///controller.lan:9090",
		"[::1]:9090":            "passthrough:///[::1]:9090",
		"dns:///kcore.lan:9090": "dns:///kcore.lan:9090",
		"unix:/run/kcore.sock":  "unix:/run/kcore.sock",
	} {
		if got := dialTarget(addr); got != want {
			t.Errorf("dialTarget(%q) = %q, want %q", addr, got, want)
		}
	}
}

type healthController struct {
	pb.UnimplementedControllerServer
}

func (healthController) GetClusterHealth(context.Context, *pb.GetClusterHealthRequest) (*pb.GetClusterHealthResponse, error) {
	return &pb.GetClusterHealthResponse{}, nil
}

func TestProbeControllerReachesALiveController(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterControllerServer(server, healthController{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	if err := probeController(context.Background(), listener.Addr().String(), insecure.NewCredentials()); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
}

func TestProbeControllerWaitsForAControllerThatStartsLate(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterControllerServer(server, healthController{})
	t.Cleanup(server.Stop)
	go func() {
		time.Sleep(700 * time.Millisecond)
		late, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		_ = server.Serve(late)
	}()

	if err := probeController(context.Background(), addr, insecure.NewCredentials()); err != nil {
		t.Fatalf("probe did not wait for the controller: %v", err)
	}
}

func TestProbeControllerGivesUpOnADeadAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := probeController(ctx, addr, insecure.NewCredentials()); err == nil {
		t.Fatal("expected an error from a closed port")
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("probe took %s, the caller's deadline was ignored", elapsed)
	}
}
