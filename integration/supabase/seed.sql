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

insert into public.sequences (id, "decimal number", character, phonetic)
values
    ( 1, '01', 'A', 'Alpha'),
    ( 2, '02', 'B', 'Bravo'),
    ( 3, '03', 'C', 'Charlie'),
    ( 4, '04', 'D', 'Delta'),
    ( 5, '05', 'E', 'Echo'),
    ( 6, '06', 'F', 'Foxtrot'),
    ( 7, '07', 'G', 'Golf'),
    ( 8, '08', 'H', 'Hotel'),
    ( 9, '09', 'I', 'India'),
    (10, '10', 'J', 'Juliet'),
    (11, '11', 'K', 'Kilo'),
    (12, '12', 'L', 'Lima'),
    (13, '13', 'M', 'Mike'),
    (14, '14', 'N', 'November'),
    (15, '15', 'O', 'Oscar'),
    (16, '16', 'P', 'Papa'),
    (17, '17', 'Q', 'Quebec'),
    (18, '18', 'R', 'Romeo'),
    (19, '19', 'S', 'Sierra'),
    (20, '20', 'T', 'Tango'),
    (21, '21', 'U', 'Uniform'),
    (22, '22', 'V', 'Victor'),
    (23, '23', 'W', 'Whiskey'),
    (24, '24', 'X', 'X-Ray'),
    (25, '25', 'Y', 'Yankee'),
    (26, '26', 'Z', 'Zulu');

-- Populate tables named using a delimited identifier.
insert into public."odd table" (id, name) values (1, 'odd table');
insert into public."a?b" (id, name) values (1, 'a?b');
insert into public."rpc/d" (id, name) values (1, 'rpc/d');
insert into public."hello//world" (id, name) values (1, 'hello//world');
insert into public."/leading" (id, name) values (1, '/leading');
insert into public."trailing/" (id, name) values (1, 'trailing/');
insert into public."e&f" (id, name) values (1, 'e&f');
insert into public."." (id, name) values (1, '.');
insert into public.".." (id, name) values (1, '..');
insert into public."../.." (id, name) values (1, '../..');
insert into public."g/../h" (id, name) values (1, 'g/../h');
insert into public."50%off" (id, name) values (1, '50%off');
insert into public."""" (id, name) values (1, '"');
insert into public."'" (id, name) values (1, '''');
insert into public."`" (id, name) values (1, '`');
insert into public."2026-06-30T11:23:31" (id, name) values (1, '2026-06-30T11:23:31');
insert into public."a+b" (id, name) values (1, 'a+b');
