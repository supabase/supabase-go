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

-- Populate rows with reserved characters.
-- - Full set from https://docs.postgrest.org/en/latest/references/api/url_grammar.html#reserved-characters:
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just comma', ',', array[',']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just dot', '.', array['.']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just colon', ':', array[':']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just asterisk', '*', array['*']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just opening parenthesis', '(', array['(']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just closing parenthesis', ')', array[')']);
-- - Extras from https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-SPECIAL-CHARS
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just dollar', '$', array['$']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just opening square bracket', '[', array['[']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just closing square bracket', ']', array[']']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just semi-colon', ';', array[';']);
-- - Other characters which AI gets excited about when considering escaping for PostgREST.
--   - "you must put double quotes around it": https://www.postgresql.org/docs/current/arrays.html#ARRAYS-INPUT
--   - "use curly braces instead of square brackets" re ov: https://docs.postgrest.org/en/latest/references/api/tables_views.html#operators
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just opening brace', '{', array['{']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just closing brace', '}', array['}']);
-- - Other characters which have common use elsewhere in the grammar of PostgreSQL.
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just caret', '^', array['^']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just percent', '%', array['%']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just left angle bracket', '<', array['<']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just right angle bracket', '>', array['>']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just equals', '=', array['=']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just plus', '+', array['+']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just minus', '-', array['-']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just double quote', '"', array['"']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just single quote', '''', array['''']);
-- - https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-STRINGS-ESCAPE
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just backslash', E'\\', array[E'\\']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just backspace', E'\b', array[E'\b']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just form feed', E'\f', array[E'\f']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just newline', E'\n', array[E'\n']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just carriage return', E'\r', array[E'\r']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just tab', E'\t', array[E'\t']);
-- - Other potentially surprising characters.
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just multitudinous', '众', array['众']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('just poop', '💩', array['💩']);

-- Populate rows with common hazard forms, more complex than just a single character.
insert into public."⚠ reserved ⚠" (name, text, "array") values ('a comma b', 'a,b', array['a,b']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('opening brace inside', 'brace{inside', array['brace{inside']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('closing brace inside', 'brace}inside', array['brace}inside']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('opening paren inside', 'paren(open', array['paren(open']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('closing paren inside', 'close)paren', array['close)paren']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('double quoted inside', 'say "hi"', array['say "hi"']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('double quoted', '"The IKEA Effect"', array['"The IKEA Effect"']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('backslash inside', 'back\slash', array['back\slash']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('space padded', ' padded ', array[' padded ']); -- aren't we all! Right?
insert into public."⚠ reserved ⚠" (name, text, "array") values ('empty', '', array['']);
insert into public."⚠ reserved ⚠" (name, text, "array") values ('wildcard version tag', 'v1.2:rc*', array['v1.2:rc*']);

insert into public.issues (id, title, status, priority, effort, resolved, assignee, tags, metadata, active_during, created_at, search)
values
    (1, 'Login button unresponsive', 'open',        1, 0.5, false, 'ada',   array['bug','ui'],              '{"severity":"high","browser":"firefox"}', tstzrange('2026-03-01T00:00:00Z', '2026-03-08T00:00:00Z', '[)'), '2026-01-01T09:00:00Z', to_tsvector('english', 'the quick brown fox jumps over the lazy dog')),
    (2, 'Dashboard chart flickers',  'open',        3, 1.5, null,  null,    array['bug','ui','charts'],     '{"severity":"low"}',                      tstzrange('2026-03-08T00:00:00Z', '2026-03-15T00:00:00Z', '[)'), '2026-01-02T09:00:00Z', to_tsvector('english', 'quick fixes ship fast')),
    (3, 'Export issues to CSV',      'in_progress', 2, 3,   false, 'grace', array['feature','backend'],     '{"severity":"medium","reviewed":true}',   tstzrange('2026-03-10T00:00:00Z', '2026-03-20T00:00:00Z', '[)'), '2026-01-03T09:00:00Z', to_tsvector('english', 'the fat cat sat on the mat')),
    (4, 'Rate limit uploads',        'triage',      5, 8,   null,  null,    array['backend','performance'], '{"severity":"high","reviewed":false}',    null,                                                            '2026-01-04T09:00:00Z', to_tsvector('english', 'the cat and the fat dog')),
    (5, 'Dark mode theme',           'resolved',    4, 2.5, true,  'ada',   array['feature','ui'],          '{"severity":"low","reviewed":true}',      tstzrange('2026-03-01T00:00:00Z', '2026-03-03T00:00:00Z', '[)'), '2026-01-05T09:00:00Z', to_tsvector('english', 'a dog day afternoon')),
    (6, 'Onboarding copy tweaks',    'closed',      2, 13,  true,  null,    array[]::text[],                null,                                      tstzrange('2026-03-15T00:00:00Z', '2026-03-22T00:00:00Z', '[)'), '2026-01-06T09:00:00Z', null);

-- The Menagerie, where:
-- - all the exotic value constructions formulate
-- - inserted data often uses an explicit cast (::type) for clarity and maintainability (less DRY, but more robust to schema drift)

insert into public."🦁 raw" (name, "byte array") values ('null', null);
insert into public."🦁 raw" (name, "byte array") values ('empty', '');
insert into public."🦁 raw" (name, "byte array") values ('zero', '\x00'::bytea);
insert into public."🦁 raw" (name, "byte array") values ('one', '\x01'::bytea);
insert into public."🦁 raw" (name, "byte array") values ('255', '\xff'::bytea);
insert into public."🦁 raw" (name, "byte array") values ('count down from ten', '\x0a09080706050403020100'::bytea);
insert into public."🦁 raw" (name, "byte array") values ('count up to ten', '\x000102030405060708090a'::bytea);

insert into public."🦁 UUID" (name, "universally unique identifier") values ('null', null);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('Nil', '00000000-0000-0000-0000-000000000000'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 canonical example', 'f81d4fae-7dec-11d0-a765-00a0c91e6bf6'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv1 example', 'c232ab00-9414-11ec-b3c8-9f6bdeced846'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv3 example', '5df41881-3aed-3515-88a7-2f4a814cf09e'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv4 example', '919108f7-52d1-4320-9bac-f847db4148a8'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv5 example', '2ed6657d-e927-568b-95e1-2665a8aea6a2'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv6 example', '1ec9414c-232a-6b00-b3c8-9f6bdeced846'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv7 example', '017f22e2-79b0-7cc3-98c4-dc0c0c07398f'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv8 time-based example', '2489e9ad-2ee2-8e00-8ec9-32d5f69181c0'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('RFC 9562 UUIDv8 name-based example', '5c146b14-3c52-8afd-938a-375d0df1fbf6'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('PostgreSQL docs example', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid);
insert into public."🦁 UUID" (name, "universally unique identifier") values ('Max', 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid);
