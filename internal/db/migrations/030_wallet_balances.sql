-- Cached wallet token balances, periodically refreshed from on-chain RPC.
CREATE TABLE IF NOT EXISTS wallet_balances (
    symbol TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    balance REAL NOT NULL DEFAULT 0.0,
    contract TEXT NOT NULL DEFAULT '',
    decimals INTEGER NOT NULL DEFAULT 18,
    is_native INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
