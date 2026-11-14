package main

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"dbsrv/internal/protocol"
)

// transportKeyVar is the environment variable the channels read.
const transportKeyVar = "TMSRV_TRANSPORT_KEY"

// transportKey is "admin transport-key": the key pair of the secure client
// transport. The private key goes to the channels (TMSRV_TRANSPORT_KEY) and
// the public one to the game folder (serverkey.txt).
//
//	admin transport-key -env .env -pub "../wyd-others/Release Cliente/serverkey.txt"
//
// With -env the key is created in that file when it has none and is never
// shown; running again reuses it, so the client file can be rewritten at any
// time. Without -env the key comes from the environment or, if there is
// none, a new pair is printed.
func transportKey(args []string) error {
	fs := flag.NewFlagSet("transport-key", flag.ExitOnError)
	envFile := fs.String("env", "", "file that holds "+transportKeyVar+" (created or completed when the key is missing)")
	pubFile := fs.String("pub", "", "where to write the public key for the client (serverkey.txt)")
	_ = fs.Parse(args)

	key, created, err := loadOrCreateTransportKey(*envFile)
	if err != nil {
		return err
	}
	public := protocol.FormatPublicKey(key.PublicKey())
	switch {
	case created && *envFile != "":
		fmt.Fprintf(os.Stderr, "%s: nova chave gravada em %s. Reinicie os canais.\n", transportKeyVar, *envFile)
	case created:
		fmt.Printf("%s=%s\n", transportKeyVar, protocol.FormatTransportKey(key))
	}
	if *pubFile != "" {
		if err := os.WriteFile(*pubFile, []byte(public+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "chave pública gravada em %s\n", *pubFile)
	}
	fmt.Fprintf(os.Stderr, "chave pública (serverkey.txt do cliente): %s\n", public)
	return nil
}

// loadOrCreateTransportKey finds the key in envFile (or, without a file, in
// the environment) and creates one when there is none.
func loadOrCreateTransportKey(envFile string) (key *ecdh.PrivateKey, created bool, err error) {
	if envFile == "" {
		if v := os.Getenv(transportKeyVar); v != "" {
			key, err = protocol.ParseTransportKey(v)
			return key, false, err
		}
		key, err = protocol.GenerateTransportKey()
		return key, true, err
	}
	content, err := os.ReadFile(envFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && name == transportKeyVar && value != "" {
			key, err = protocol.ParseTransportKey(value)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", envFile, err)
			}
			return key, false, nil
		}
	}
	if key, err = protocol.GenerateTransportKey(); err != nil {
		return nil, false, err
	}
	// Appended, never rewritten: the file has the other secrets.
	f, err := os.OpenFile(envFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, false, err
	}
	line := fmt.Sprintf("%s=%s\n", transportKeyVar, protocol.FormatTransportKey(key))
	if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
		line = "\n" + line
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return nil, false, err
	}
	return key, true, f.Close()
}
