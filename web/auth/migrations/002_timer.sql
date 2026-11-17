-- The running stopwatch of a person: at most one, kept on the server so it runs on when the phone sleeps. Stopping it
-- books an entry in the core library; this table only knows that a clock runs.
CREATE TABLE epd_timer (
    account_id  text PRIMARY KEY REFERENCES epd_account(id) ON DELETE CASCADE,
    owner       text NOT NULL,
    employer_id text NOT NULL,
    project_id  text NOT NULL DEFAULT '',
    note        text NOT NULL DEFAULT '',
    started     timestamptz NOT NULL
);
