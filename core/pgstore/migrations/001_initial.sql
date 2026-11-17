-- SPDX-License-Identifier: MIT
-- The tables of EightPerDay. {{prefix}} is the table prefix and {{tenant_setting}} the setting that carries the tenant
-- of a connection; both are chosen by the application and checked before they are put in (pgstore.Options).
--
-- Every table carries its tenant, filled from the setting, and row-level security keeps each tenant to its rows: a
-- connection without the setting sees nothing and can write nothing (the tenant column is NOT NULL). FORCE applies the
-- rules to the owner of the tables as well. Keys carry the tenant in front, so IDs never collide across tenants.
--
-- The rows keep the whole record as JSON in doc; the other columns are copies for filtering, written together with it.

CREATE TABLE {{prefix}}records (
    tenant      text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    kind        text NOT NULL,
    id          text NOT NULL,
    owner       text NOT NULL,
    employer_id text NOT NULL DEFAULT '',
    doc         jsonb NOT NULL,
    PRIMARY KEY (tenant, kind, id)
);
CREATE INDEX {{prefix}}records_of_owner ON {{prefix}}records (tenant, owner, kind);

CREATE TABLE {{prefix}}entries (
    tenant      text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    id          text NOT NULL,
    owner       text NOT NULL,
    employer_id text NOT NULL,
    local_date  date NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('work', 'break', 'adjust')),
    minutes     integer NOT NULL,
    start_utc   timestamptz,
    end_utc     timestamptz,
    project_id  text NOT NULL DEFAULT '',
    version     integer NOT NULL CHECK (version >= 1),
    created     timestamptz NOT NULL,
    deleted_at  timestamptz,
    doc         jsonb NOT NULL,
    PRIMARY KEY (tenant, id)
);
CREATE INDEX {{prefix}}entries_by_day ON {{prefix}}entries (tenant, owner, local_date);

CREATE TABLE {{prefix}}absences (
    tenant      text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    id          text NOT NULL,
    owner       text NOT NULL,
    employer_id text NOT NULL DEFAULT '',
    type        text NOT NULL,
    date_from   date NOT NULL,
    date_to     date NOT NULL CHECK (date_to >= date_from),
    version     integer NOT NULL CHECK (version >= 1),
    deleted_at  timestamptz,
    doc         jsonb NOT NULL,
    PRIMARY KEY (tenant, id)
);
CREATE INDEX {{prefix}}absences_by_day ON {{prefix}}absences (tenant, owner, date_from, date_to);

-- The correction history. Rows are only ever added, and removed by the retention purge or an erasure.
CREATE TABLE {{prefix}}revisions (
    tenant      text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    id          text NOT NULL,
    seq         bigint GENERATED ALWAYS AS IDENTITY,
    owner       text NOT NULL,
    record_type text NOT NULL CHECK (record_type IN ('entry', 'absence')),
    record_id   text NOT NULL,
    version     integer NOT NULL,
    op          text NOT NULL CHECK (op IN ('create', 'update', 'delete')),
    at          timestamptz NOT NULL,
    doc         jsonb NOT NULL,
    PRIMARY KEY (tenant, id)
);
CREATE INDEX {{prefix}}revisions_of_record ON {{prefix}}revisions (tenant, owner, record_id, seq);

ALTER TABLE {{prefix}}records ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}records FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}records
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));

ALTER TABLE {{prefix}}entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}entries FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}entries
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));

ALTER TABLE {{prefix}}absences ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}absences FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}absences
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));

ALTER TABLE {{prefix}}revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}revisions
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));
