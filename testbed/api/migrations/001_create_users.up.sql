-- Runtime role for the API. It is NOT a superuser, so row-level security applies to it.
create role app login password 'app';

create table users (
  id            bigint generated always as identity primary key,
  email         text not null unique,
  password_hash text not null,
  created_at    timestamptz not null default now()
);

-- RLS-style policy: a request can only see the row of the user it runs as.
-- The API sets app.user_id per transaction from the JWT `sub`.
alter table users enable row level security;

create policy users_select_self on users
  for select to app
  using (id = nullif(current_setting('app.user_id', true), '')::bigint);

grant select on users to app;

-- Signup and login happen before we know who the user is, so they go through
-- SECURITY DEFINER functions (run as the table owner, bypassing RLS) instead of
-- giving `app` broad access to the table.
create function app_signup(p_email text, p_password_hash text) returns bigint
  language sql security definer set search_path = public
  as $$ insert into users (email, password_hash) values (p_email, p_password_hash) returning id $$;

create function app_find_login(p_email text) returns table (id bigint, password_hash text)
  language sql security definer set search_path = public
  as $$ select id, password_hash from users where email = p_email $$;

revoke execute on function app_signup(text, text), app_find_login(text) from public;
grant execute on function app_signup(text, text), app_find_login(text) to app;
