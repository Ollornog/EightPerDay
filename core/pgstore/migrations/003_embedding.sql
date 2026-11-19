-- SPDX-License-Identifier: MIT
-- What an application that embeds the library needs (M-3).
--
-- The access log keeps its rules, written the way a guard of the embedding application reads them: one permissive
-- policy for every command that keeps to the tenant, like every other table, and restrictive policies on top that
-- narrow it. No row changes (UPDATE sees no row), and DELETE reaches only rows older than the minimum retention of two
-- years, or every row of a tenant whose deletion for good the application started in its transaction by setting
-- {{tenant_setting}}_erase to that tenant (pgstore.Options.EraseSetting).
DROP POLICY {{prefix}}access_read ON {{prefix}}access_log;
DROP POLICY {{prefix}}access_add ON {{prefix}}access_log;
DROP POLICY {{prefix}}access_expire ON {{prefix}}access_log;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}access_log
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));
CREATE POLICY {{prefix}}access_unchanged ON {{prefix}}access_log AS RESTRICTIVE FOR UPDATE
    USING (false);
CREATE POLICY {{prefix}}access_expire ON {{prefix}}access_log AS RESTRICTIVE FOR DELETE
    USING (at < now() - interval '2 years' OR tenant = current_setting('{{tenant_setting}}_erase', true));

-- The running stopwatch of a person (core.Timer): at most one per person and tenant.
CREATE TABLE {{prefix}}timers (
    tenant      text NOT NULL DEFAULT current_setting('{{tenant_setting}}', true),
    owner       text NOT NULL,
    employer_id text NOT NULL,
    project_id  text NOT NULL DEFAULT '',
    note        text NOT NULL DEFAULT '',
    started     timestamptz NOT NULL,
    PRIMARY KEY (tenant, owner)
);

ALTER TABLE {{prefix}}timers ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{prefix}}timers FORCE ROW LEVEL SECURITY;
CREATE POLICY {{prefix}}tenant_only ON {{prefix}}timers
    USING (tenant = current_setting('{{tenant_setting}}', true))
    WITH CHECK (tenant = current_setting('{{tenant_setting}}', true));
