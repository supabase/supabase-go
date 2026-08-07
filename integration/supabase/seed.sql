-- Seed data for the Go SDK integration tests, applied automatically on
-- Supabase stack start after the migrations in migrations/ have created
-- the schema.
insert into public.instruments (name, acquired_year)
values ('violin', 2015), ('viola', 2020), ('cello', null);
