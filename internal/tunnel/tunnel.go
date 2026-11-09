// Package tunnel opens a local TCP port that forwards through SSH to the
// database. Dump tools then connect to 127.0.0.1 and the backup stream still
// never touches a local file.
package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

type Spec struct {
	Host     string
	Port     int
	User     string
	Password string
	Key      string
}

func Start(ctx context.Context, spec Spec, targetHost string, targetPort int) (string, error) {
	if spec.Port == 0 {
		spec.Port = 22
	}
	var auth []ssh.AuthMethod
	if spec.Key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(spec.Key))
		if err != nil {
			return "", fmt.Errorf("ssh key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if spec.Password != "" {
		auth = append(auth, ssh.Password(spec.Password))
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("ssh password or key is required")
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(spec.Host, fmt.Sprint(spec.Port)), &ssh.ClientConfig{
		User:            spec.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	})
	if err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return "", err
	}
	go func() {
		<-ctx.Done()
		listener.Close()
		client.Close()
	}()
	target := net.JoinHostPort(targetHost, fmt.Sprint(targetPort))
	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			go bridge(client, local, target)
		}
	}()
	return listener.Addr().String(), nil
}

func bridge(client *ssh.Client, local net.Conn, target string) {
	remote, err := client.Dial("tcp", target)
	if err != nil {
		local.Close()
		return
	}
	go func() {
		io.Copy(remote, local)
		remote.Close()
	}()
	io.Copy(local, remote)
	local.Close()
}
