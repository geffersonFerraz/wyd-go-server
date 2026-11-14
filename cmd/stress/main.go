// Command stress is a load test for TMSRV: it logs in many accounts at once
// through the real client protocol (secure transport, login, numeric
// password, character creation, entering the world) and keeps them walking
// around, printing the counters every few seconds.
//
//	stress create -dsn postgres://... -n 300 [-prefix BOT] [-password 1234] [-numeric 123456]
//	                       # creates BOT0001..BOT0300 (existing ones are kept)
//	stress run -addr 127.0.0.1:8281 -pubkey-file serverkey.txt -n 300 [-rate 20]
//	                       # connects them; Ctrl-C disconnects all
//
// The server's public key comes from -pubkey (64 hex digits), -pubkey-file
// (the client's serverkey.txt) or, with neither, TMSRV_TRANSPORT_KEY.
//
// Each bot uses its own random MAC, so login.max_accounts_per_computer does
// not get in the way. TMSRV holds at most 999 players per channel.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "create":
		err = createAccounts(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `uso:
  stress create -dsn <postgres> -n <contas> [-prefix BOT] [-start 1] [-password 1234] [-numeric 123456]
  stress run    -addr <host:porta> -n <contas> [-pubkey-file serverkey.txt] [-rate 20] ...

"stress <comando> -h" mostra todas as opções.`)
	os.Exit(2)
}

// accountName is the login of bot i: prefix and a 4-digit number (the
// client limits logins to 4..12 letters and digits).
func accountName(prefix string, i int) string {
	return fmt.Sprintf("%s%04d", prefix, i)
}
