package grpc

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

var (
	errTrustedPeerRequired    = errors.New("需要已验证的 mTLS 客户端证书")
	errCallerIdentityMismatch = errors.New("mTLS 客户端身份与来源服务不一致")
)

func trustedCallerFromPeer(ctx context.Context, claimedService string) (string, error) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return "", errTrustedPeerRequired
	}
	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", errTrustedPeerRequired
	}
	claimedService = strings.TrimSpace(claimedService)
	if claimedService == "" || tlsInfo.State.PeerCertificates[0].VerifyHostname(claimedService) != nil {
		return "", errCallerIdentityMismatch
	}
	return claimedService, nil
}
