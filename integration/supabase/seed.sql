-- Seed data for the Go SDK integration tests, applied automatically on
-- Supabase stack start after the migrations in migrations/ have created
-- the schema.
insert into public.instruments (name, acquired_year)
values ('violin', 2015), ('viola', 2020), ('cello', null);

insert into public.players (id, section, seat, rating, tenure)
values
    (1, 2, 10, 90, null),
    (2, 1, 30, null, 40),
    (3, 2, 20, 50, 30),
    (4, 1, 5, 60, null),
    (5, 3, 50, null, 20),
    (6, 3, 60, 20, null);
