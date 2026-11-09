package app

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"dmsx/internal/tunnel"
)

func dialSSH(ctx context.Context, spec SSH, targetHost string, targetPort int) (string, error) {
	return tunnel.Start(ctx, tunnel.Spec{
		Host:     spec.Host,
		Port:     spec.Port,
		User:     spec.User,
		Password: spec.Password,
		Key:      spec.Key,
	}, targetHost, targetPort)
}

func splitAddr(addr string) (string, int, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, fmt.Errorf("local port: %w", err)
	}
	return host, n, nil
}
