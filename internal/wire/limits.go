package wire

// MaxLimit is the highest value a configurable game limit may take (gold,
// damage, HP, MP, attack, defense). It leaves room for the percentage and
// multiplier arithmetic of the combat formulas to stay inside int64, and it
// is still exact in the admin panel's JavaScript numbers.
const MaxLimit = 1_000_000_000_000_000
