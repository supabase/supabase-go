-- Seed data for the Go SDK integration tests, applied automatically on
-- Supabase stack start after the migrations in migrations/ have created
-- the schema.
insert into public.instruments (name)
values ('violin'), ('viola'), ('cello');
