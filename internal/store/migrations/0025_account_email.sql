-- Optional address given at public registration; used to recover the account.
ALTER TABLE accounts ADD COLUMN email text;
