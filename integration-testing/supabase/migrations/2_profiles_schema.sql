-- Profiles mirror of auth.users for the admin-provisioning example program,
-- the Managing User Data pattern from the Supabase docs: application code
-- never queries the auth schema directly, so a trigger copies each new
-- account into public.profiles, where PostgREST can serve it. Row Level
-- Security is enabled with no policies, leaving the mirror readable only to
-- the service role - the secret API key - which bypasses RLS.
create table public.profiles (
    id uuid not null primary key references auth.users (id) on delete cascade,
    email text
);
alter table public.profiles enable row level security;

create function public.handle_new_user()
returns trigger
set search_path = ''
language plpgsql
security definer
as $$
begin
    insert into public.profiles (id, email) values (new.id, new.email);
    return new;
end;
$$;

create trigger on_auth_user_created
    after insert on auth.users
    for each row execute procedure public.handle_new_user();
