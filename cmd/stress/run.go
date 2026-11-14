package main

import (
	"context"
	"crypto/ecdh"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"dbsrv/internal/protocol"
)

// run is "stress run": connect the bots and report until Ctrl-C or
// -duration.
func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8281", "endereço da TMSRV (host:porta)")
	pubkey := fs.String("pubkey", "", "chave pública do servidor (64 dígitos hex)")
	pubkeyFile := fs.String("pubkey-file", "", "arquivo com a chave pública (o serverkey.txt do cliente)")
	n := fs.Int("n", 10, "quantidade de bots")
	start := fs.Int("start", 1, "número da primeira conta")
	prefix := fs.String("prefix", "BOT", "prefixo do login das contas")
	password := fs.String("password", "1234", "senha das contas")
	numeric := fs.String("numeric", "123456", "senha numérica das contas")
	rate := fs.Float64("rate", 20, "bots iniciados por segundo (rampa)")
	slot := fs.Int("slot", 0, "slot do personagem (0-3); vazio = cria um")
	class := fs.Int("class", -1, "classe do personagem criado (0-3, -1 = aleatória)")
	move := fs.Duration("move", 2*time.Second, "intervalo entre passos no mundo (0 = parado)")
	wander := fs.Int("wander", 12, "distância máxima do ponto de entrada, em células")
	chat := fs.Duration("chat", 0, "intervalo entre mensagens no chat (0 = nunca)")
	timeout := fs.Duration("timeout", 30*time.Second, "tempo máximo de cada etapa do login")
	reconnect := fs.Bool("reconnect", false, "reconectar quando a sessão cair ou falhar")
	reconnectGap := fs.Duration("reconnect-delay", 5*time.Second, "espera antes de reconectar")
	duration := fs.Duration("duration", 0, "tempo total do teste (0 = até Ctrl-C)")
	every := fs.Duration("report", 5*time.Second, "intervalo do relatório")
	_ = fs.Parse(args)

	if *n <= 0 || *rate <= 0 {
		return errors.New("-n e -rate devem ser maiores que zero")
	}
	if *slot < 0 || *slot > 3 || *class > 3 {
		return errors.New("-slot vai de 0 a 3 e -class de -1 a 3")
	}
	if *n > 999 {
		fmt.Fprintln(os.Stderr, "aviso: a TMSRV aceita no máximo 999 jogadores por canal")
	}
	key, err := serverKey(*pubkey, *pubkeyFile)
	if err != nil {
		return err
	}
	if *move > 0 && *move < time.Second {
		*move = time.Second // TMSRV ignores steps less than 900 ms apart
	}

	cfg := &botConfig{
		addr: *addr, serverKey: key, password: *password, numeric: *numeric,
		slot: *slot, class: *class, move: *move, wander: max(*wander, 1), chat: *chat,
		stepTimeout: *timeout, reconnect: *reconnect, reconnectGap: *reconnectGap,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	st := newStats()
	rep := &reporter{s: st, total: *n, begin: time.Now(), last: time.Now()}
	fmt.Printf("%d bots (%s .. %s) em %s, %.0f/s\n", *n,
		accountName(*prefix, *start), accountName(*prefix, *start+*n-1), *addr, *rate)

	var wg sync.WaitGroup
	go func() { // ramp
		gap := time.Duration(float64(time.Second) / *rate)
		for i := 0; i < *n && ctx.Err() == nil; i++ {
			b := newBot(cfg, st, accountName(*prefix, *start+i))
			wg.Go(func() { b.live(ctx) })
			sleep(ctx, gap)
		}
	}()

	tick := time.NewTicker(*every)
	defer tick.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-tick.C:
			fmt.Println(rep.line())
		}
	}
	fmt.Println("encerrando os bots...")
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		fmt.Println("alguns bots não encerraram a tempo")
	}
	fmt.Print(rep.final())
	return nil
}

// serverKey reads the server's public key from the flags or derives it
// from TMSRV_TRANSPORT_KEY.
func serverKey(hex, file string) (*ecdh.PublicKey, error) {
	switch {
	case hex != "":
		return protocol.ParsePublicKey(hex)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return protocol.ParsePublicKey(string(b))
	}
	if env := os.Getenv("TMSRV_TRANSPORT_KEY"); env != "" {
		k, err := protocol.ParseTransportKey(env)
		if err != nil {
			return nil, fmt.Errorf("TMSRV_TRANSPORT_KEY: %w", err)
		}
		return k.PublicKey(), nil
	}
	return nil, errors.New("informe -pubkey, -pubkey-file ou TMSRV_TRANSPORT_KEY")
}
