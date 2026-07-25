package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func verifiedCallerContext(service string) context.Context {
	certificate := &x509.Certificate{DNSNames: []string{service}}
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{certificate},
			VerifiedChains:   [][]*x509.Certificate{{certificate}},
		}},
	})
}

func TestTrustedCallerFromPeerRequiresVerifiedMatchingSAN(t *testing.T) {
	caller, err := trustedCallerFromPeer(verifiedCallerContext("shop-user"), "shop-user")
	require.NoError(t, err)
	require.Equal(t, "shop-user", caller)

	_, err = trustedCallerFromPeer(verifiedCallerContext("shop-user"), "shop-order")
	require.ErrorIs(t, err, errCallerIdentityMismatch)

	_, err = trustedCallerFromPeer(context.Background(), "shop-user")
	require.ErrorIs(t, err, errTrustedPeerRequired)
}

func TestServerCallInjectsVerifiedCallerIdentity(t *testing.T) {
	server := &Server{handler: func(ctx context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		caller, trusted := coretypes.TrustedInternalCallerFromContext(ctx)
		require.True(t, trusted)
		require.Equal(t, "shop-user", caller)
		return []byte("ok"), nil
	}}

	response, err := server.Call(verifiedCallerContext("shop-user"), &pb.PayloadRequest{SourceService: "shop-user"})

	require.NoError(t, err)
	require.Equal(t, []byte("ok"), response.Data)
}

func TestServerCallRejectsMismatchedVerifiedCallerBeforeHandler(t *testing.T) {
	called := false
	server := &Server{handler: func(context.Context, *coretypes.PayLoad) ([]byte, error) {
		called = true
		return []byte("unexpected"), nil
	}}

	_, err := server.Call(verifiedCallerContext("shop-order"), &pb.PayloadRequest{SourceService: "shop-user"})

	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.False(t, called)
}
