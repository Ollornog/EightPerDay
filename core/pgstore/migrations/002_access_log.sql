-- SPDX-License-Identifier: MIT
-- The access log: who read or changed whose data when (core.Access). Rows are only added; row-level security has no
-- policy for UPDATE, so no row changes, and lets DELETE only reach rows older than the minimum retention of two
-- years (the retention purge). FORCE holds this for the owner of the table as well; only a superuser or a role with
-- BYPASSRLS gets past it, so the store must not run as one.

CREATE TABLE {{prefix}}access_log (
    tenant  text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    id      text NOT NULL,
    seq     bigint GENERATED ALWAYS AS IDENTITY,
    subject text NOT NULL,
    at      timestamptz NOT NULL,
    hash    text NOT NULL,
    doc     jsonb NOT NULL,
    PRIMARY KEY (tenant, id)
);
CREATE INDEX {{prefix}}access_log_of_subject ON {{prefix}}access_log (tenant, subject, seq);

ALTER TABLE {{prefix}}access_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}access_log FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}access_read ON {{prefix}}access_log FOR SELECT
    USING (tenant = current_setting('{{tenant_setting}}', true));
CREATE POLICY {{prefix}}access_add ON {{prefix}}access_log FOR INSERT
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));
CREATE POLICY {{prefix}}access_expire ON {{prefix}}access_log FOR DELETE
    USING (tenant = current_setting('{{tenant_setting}}', true) AND at < now() - interval '2 years');
