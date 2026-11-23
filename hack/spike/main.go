// Command spike is milestone 1's check against a real SMB server: log in,
// list the shares, list a folder, stream a file and measure the throughput.
// It is a tool for the maintainers, not part of ferry.
//
//	go run ./hack/spike -server nas.example.lan:445 -user alex -share alex -path Photos -file big.bin
//
// The password is read from the terminal without echo and used for the one
// session setup; it is not printed, logged or stored. It also tries one login
// as a user that cannot exist, to show how the server treats unknown users
// (ferry must refuse a guest session either way).
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/denisneuling/ferry/internal/smb"
)

func main() {
	server := flag.String("server", "", "host:port of the SMB server")
	user := flag.String("user", "", "user name")
	share := flag.String("share", "", "share to list (optional)")
	path := flag.String("path", "", "folder in the share to list (optional)")
	file := flag.String("file", "", "file in the share to stream (optional)")
	flag.Parse()
	if *server == "" || *user == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*server, *user, *share, *path, *file); err != nil {
		fmt.Fprintln(os.Stderr, "spike:", err)
		os.Exit(1)
	}
}

func readPassword() (string, error) {
	fmt.Fprint(os.Stderr, "password: ")
	defer fmt.Fprintln(os.Stderr)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	return line, nil
}

func step(name string, start time.Time, err error) {
	if err != nil {
		fmt.Printf("%-22s FAILED after %v: %v\n", name, time.Since(start).Round(time.Millisecond), err)
		return
	}
	fmt.Printf("%-22s ok (%v)\n", name, time.Since(start).Round(time.Millisecond))
}

func run(server, user, share, path, file string) error {
	ctx := context.Background()
	opts := smb.LoginOptions{Server: server}

	// An unknown user: a server with `map to guest = Bad User` would offer
	// a guest session, which ferry must turn down (KindGuest).
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	t := time.Now()
	_, err := smb.Login(ctx, opts, smb.Credentials{User: "ferry-spike-" + hex.EncodeToString(b), Password: "x"})
	fmt.Printf("%-22s refused as %s (%v)\n", "unknown user", smb.KindOf(err), time.Since(t).Round(time.Millisecond))
	if err == nil {
		return errors.New("an unknown user got a session")
	}

	pw, err := readPassword()
	if err != nil {
		return err
	}
	t = time.Now()
	s, err := smb.Login(ctx, opts, smb.Credentials{User: user, Password: pw})
	step("login", t, err)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	t = time.Now()
	shares, err := s.Shares(ctx)
	step("shares", t, err)
	for _, sh := range shares {
		fmt.Printf("    %-20s %s\n", sh.Name, sh.Comment)
	}

	if share != "" {
		t = time.Now()
		entries, err := s.ReadDir(ctx, share, path, 100000)
		step("list folder", t, err)
		fmt.Printf("    %d entries\n", len(entries))
		for i, e := range entries {
			if i == 10 {
				fmt.Printf("    …\n")
				break
			}
			kind := "file"
			if e.Dir {
				kind = "dir "
			}
			fmt.Printf("    %s %12d %s %s\n", kind, e.Size, e.ModTime.Format(time.DateTime), e.Name)
		}
	}

	if share != "" && file != "" {
		t = time.Now()
		f, err := s.Open(ctx, share, file)
		step("open file", t, err)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		t = time.Now()
		n, err := io.Copy(io.Discard, f)
		el := time.Since(t)
		step("stream file", t, err)
		fmt.Printf("    %d bytes in %v: %.1f MB/s\n", n, el.Round(time.Millisecond), float64(n)/el.Seconds()/1e6)
	}
	return nil
}
