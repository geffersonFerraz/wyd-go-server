package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/store"
)

var (
	loginRe   = regexp.MustCompile(`^[A-Za-z0-9]{4,12}$`)
	passRe    = regexp.MustCompile(`^[\x21-\x7e]{4,10}$`)
	numericRe = regexp.MustCompile(`^[0-9]{4,6}$`)
)

// createAccounts is "stress create": the bot accounts go straight into the
// database, with the password and numeric password already set.
func createAccounts(args []string) error {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("DATABASE_URL"), "Postgres (ou DATABASE_URL)")
	n := fs.Int("n", 0, "quantidade de contas")
	start := fs.Int("start", 1, "número da primeira conta")
	prefix := fs.String("prefix", "BOT", "prefixo do login (a conta i é <prefixo><i com 4 dígitos>)")
	password := fs.String("password", "1234", "senha de todas as contas")
	numeric := fs.String("numeric", "123456", "senha numérica de todas as contas")
	_ = fs.Parse(args)

	if *dsn == "" {
		return errors.New("-dsn (ou DATABASE_URL) é obrigatório")
	}
	if *n <= 0 || *start < 0 {
		return errors.New("-n deve ser maior que zero")
	}
	if last := accountName(*prefix, *start+*n-1); !loginRe.MatchString(accountName(*prefix, *start)) || !loginRe.MatchString(last) {
		return fmt.Errorf("login inválido %q: use letras e dígitos, 4 a 12 caracteres no total", last)
	}
	if !passRe.MatchString(*password) {
		return errors.New("-password deve ter 4 a 10 caracteres ASCII visíveis")
	}
	if !numericRe.MatchString(*numeric) {
		return errors.New("-numeric deve ter 4 a 6 dígitos")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dsn, time.Local)
	if err != nil {
		return err
	}
	defer st.Close()

	// One hash for every account: hashing is deliberately slow.
	pwHash, err := auth.Hash(*password)
	if err != nil {
		return err
	}
	numHash, err := auth.Hash(*numeric)
	if err != nil {
		return err
	}

	created, kept := 0, 0
	for i := *start; i < *start+*n; i++ {
		ok, err := st.CreateAccount(ctx, accountName(*prefix, i), pwHash, &numHash, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", accountName(*prefix, i), err)
		}
		if ok {
			created++
		} else {
			kept++
		}
	}
	fmt.Printf("%d contas criadas, %d já existiam (%s .. %s)\n", created, kept,
		accountName(*prefix, *start), accountName(*prefix, *start+*n-1))
	return nil
}
