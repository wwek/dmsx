package engine

import (
	"context"
	"io"
	"sync"
)

// publicKeyFlag asks the client to fetch the server's RSA public key. A
// caching_sha2_password login needs it to sign in over a plain connection,
// which is the connection dmsx makes when the operator did not ask for TLS.
// A client older than MySQL 8.0.4 does not know the flag.
const publicKeyFlag = "--get-server-public-key"

var publicKeySupport sync.Map

// clientKnowsPublicKey reports whether the client binary accepts the public
// key flag. The answer depends on the client and not on the server, so each
// binary is asked once.
func clientKnowsPublicKey(ctx context.Context, bin string) bool {
	path, err := look(bin)
	if err != nil {
		return false
	}
	if cached, ok := publicKeySupport.Load(path); ok {
		return cached.(bool)
	}
	_, err = runCapture(ctx, path, []string{publicKeyFlag, "--version"}, nil, nil, io.Discard, 16<<10)
	known := err == nil
	publicKeySupport.Store(path, known)
	return known
}

// myClientArgs are the arguments that every MySQL client call carries.
// MariaDB has no caching_sha2_password and its client does not know the flag.
func myClientArgs(ctx context.Context, bin string, maria bool) []string {
	if maria || !clientKnowsPublicKey(ctx, bin) {
		return nil
	}
	return []string{publicKeyFlag}
}
