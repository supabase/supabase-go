-- Seed data for the SDK integration tests, applied automatically on stack
-- start after the migrations in migrations/ have created the schema. Data
-- only: the CLI sends this file as one batch (see the migration's header).
insert into public.instruments (name)
values ('violin'), ('viola'), ('cello');
