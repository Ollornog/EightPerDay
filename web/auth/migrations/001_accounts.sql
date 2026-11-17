-- Accounts of the standalone program. The core library knows nothing about sign-in (ADR-1), so these tables belong to
-- the program, not to the library, and carry their own prefix.

CREATE TABLE epd_account (
    id            text PRIMARY KEY,
    username      text NOT NULL UNIQUE,
    display_name  text NOT NULL,
    admin         boolean NOT NULL DEFAULT false,
    -- The user handle of WebAuthn: random, never shown, never derived from the name.
    webauthn_id   bytea NOT NULL UNIQUE,
    -- Argon2id in the PHC string format, or NULL when the account signs in without a password.
    password_hash text,
    -- The TOTP secret (base32), only set once a code from it was confirmed.
    totp_secret   text,
    -- The last time step a TOTP code was accepted for, so one code works only once.
    totp_last_step bigint NOT NULL DEFAULT 0,
    language      text NOT NULL DEFAULT '',
    disabled      boolean NOT NULL DEFAULT false,
    created       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE epd_passkey (
    id         bytea PRIMARY KEY,
    account_id text NOT NULL REFERENCES epd_account(id) ON DELETE CASCADE,
    name       text NOT NULL,
    -- The credential as go-webauthn keeps it: public key, counter, flags, transports.
    credential jsonb NOT NULL,
    created    timestamptz NOT NULL DEFAULT now(),
    last_used  timestamptz
);
CREATE INDEX epd_passkey_account ON epd_passkey (account_id);

-- Sessions keep only the SHA-256 of their token: a copy of the table signs nobody in.
CREATE TABLE epd_session (
    token_hash bytea PRIMARY KEY,
    account_id text NOT NULL REFERENCES epd_account(id) ON DELETE CASCADE,
    created    timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    expires    timestamptz NOT NULL
);
CREATE INDEX epd_session_account ON epd_session (account_id);

-- One-time links: the first administrator, an invitation, or a reset of an account's sign-in.
CREATE TABLE epd_setup_link (
    token_hash bytea PRIMARY KEY,
    -- NULL for a new account; for a reset, the account whose sign-in is replaced.
    account_id text REFERENCES epd_account(id) ON DELETE CASCADE,
    -- The name of the new account, or empty when the person chooses it.
    username   text NOT NULL DEFAULT '',
    admin      boolean NOT NULL DEFAULT false,
    created    timestamptz NOT NULL DEFAULT now(),
    expires    timestamptz NOT NULL
);
