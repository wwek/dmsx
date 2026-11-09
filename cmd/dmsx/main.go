package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"dmsx/internal/app"
	"dmsx/internal/server"
)

const usage = `usage: dmsx [command]

commands:
  serve            run the console and the scheduler (default)
  reset-password   set a new operator password, read from the terminal or stdin
  healthcheck      exit 0 when the console on DMSX_ADDR answers, 1 otherwise
`

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "serve":
		serve()
	case "reset-password":
		os.Exit(resetPassword(os.Stdin, os.Stderr))
	case "healthcheck":
		os.Exit(healthcheck(os.Stderr))
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func serve() {
	addr := env("DMSX_ADDR", "127.0.0.1:8080")
	data := env("DMSX_DATA", "data")
	application, err := app.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go application.Schedule(ctx)
	go application.Watch(ctx)

	httpServer := &http.Server{Addr: addr, Handler: server.New(application), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	log.Printf("disclaimer: dmsx can delete and replace database data. Loss is the operator's responsibility. Do not blame the author. %s", "https://github.com/wwek/dmsx")
	log.Printf("dmsx listening on http://%s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// resetPassword reads the password rather than taking it as an argument, so it
// does not show up in the process list or the shell history.
func resetPassword(stdin *os.File, stderr io.Writer) int {
	password, err := readPassword(stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	application, err := app.Open(env("DMSX_DATA", "data"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer application.Close()
	if err := application.ResetOperatorPassword(context.Background(), password); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stderr, "password changed; open browser sessions are signed out")
	return 0
}

func readPassword(stdin *os.File, stderr io.Writer) (string, error) {
	fd := int(stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(stderr, "new password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(stderr, "again: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("the two passwords differ")
	}
	return string(first), nil
}

func healthcheck(stderr io.Writer) int {
	addr := env("DMSX_ADDR", "127.0.0.1:8080")
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, resp.Status)
		return 1
	}
	return 0
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
