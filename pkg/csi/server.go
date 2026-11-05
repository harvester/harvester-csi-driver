/*
This file is copied then modified from
    https://github.com/kubernetes-csi/csi-driver-host-path/blob/master/pkg/hostpath/server.go
*/

/*
Copyright 2019 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package csi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func NewNonBlockingGRPCServer() *NonBlockingGRPCServer {
	return &NonBlockingGRPCServer{}
}

type NonBlockingGRPCServer struct {
	wg     sync.WaitGroup
	server *grpc.Server
}

func (s *NonBlockingGRPCServer) Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {
	listener := s.listen(endpoint, ids, cs, ns)

	s.wg.Add(1)

	go s.serve(listener)

}

func (s *NonBlockingGRPCServer) Wait() {
	s.wg.Wait()
}

func (s *NonBlockingGRPCServer) Stop() {
	s.server.GracefulStop()
}

func (s *NonBlockingGRPCServer) ForceStop() {
	s.server.Stop()
}

// StopOnDone blocks until ctx is done, then stops the server gracefully,
// falling back to a forced stop if in-flight RPCs have not finished within
// timeout.
func (s *NonBlockingGRPCServer) StopOnDone(ctx context.Context, timeout time.Duration) {
	<-ctx.Done()
	logrus.Info("Stopping GRPC server")

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(timeout):
		logrus.Warnf("GRPC server did not stop within %s, forcing stop", timeout)
		s.ForceStop()
	}
}

// listen creates the listener and the server before Start returns, so that
// Stop and ForceStop never see a nil server.
func (s *NonBlockingGRPCServer) listen(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) net.Listener {

	proto, addr, err := parseEndpoint(endpoint)
	if err != nil {
		logrus.Fatal(err.Error())
	}

	if proto == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			logrus.Fatalf("Failed to remove %s, error: %s", addr, err.Error())
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		logrus.Fatalf("Failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logGRPC),
	}
	server := grpc.NewServer(opts...)
	s.server = server

	if ids != nil {
		csi.RegisterIdentityServer(server, ids)
	}
	if cs != nil {
		csi.RegisterControllerServer(server, cs)
	}
	if ns != nil {
		csi.RegisterNodeServer(server, ns)
	}

	return listener
}

func (s *NonBlockingGRPCServer) serve(listener net.Listener) {
	defer s.wg.Done()

	logrus.Infof("Listening for connections on address: %#v", listener.Addr())

	if err := s.server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		logrus.Fatalf("Failed to serve: %v", err)
	}
}

func parseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("invalid endpoint: %v", ep)
}

func logGRPC(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	log := logrus.StandardLogger()
	var logLevel logrus.Level
	switch info.FullMethod {
	case "/csi.v1.Node/NodeGetCapabilities", "/csi.v1.Node/NodeGetVolumeStats":
		logLevel = logrus.TraceLevel
	default:
		logLevel = logrus.InfoLevel
	}

	log.Logf(logLevel, "GRPC call: %s request: %+v", info.FullMethod, protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		if logLevel == logrus.TraceLevel {
			log.Errorf("GRPC call: %s request: %+v failed with error: %v", info.FullMethod, protosanitizer.StripSecrets(req), err)
		} else {
			log.Errorf("GRPC error: %v", err)
		}
	} else {
		log.Logf(logLevel, "GRPC response: %+v", protosanitizer.StripSecrets(resp))
	}
	return resp, err
}
