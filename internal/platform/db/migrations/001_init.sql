create table users (
    id            bigserial primary key,
    username      text not null unique,
    full_name     text not null default '',
    password_hash text not null,
    role          text not null check (role in ('admin', 'operator', 'viewer')),
    disabled      boolean not null default false,
    created_at    timestamptz not null default now(),
    last_login    timestamptz
);

create table api_tokens (
    id         bigserial primary key,
    user_id    bigint not null references users(id) on delete cascade,
    name       text not null,
    token_hash bytea not null unique,
    created_at timestamptz not null default now(),
    last_used  timestamptz,
    expires_at timestamptz
);

-- Каждая версия конфига хранится целиком: diff и откат считаются от тела.
create table config_revisions (
    id               bigserial primary key,
    created_at       timestamptz not null default now(),
    author           text not null,
    comment          text not null default '',
    body             jsonb not null,
    state            text not null check (state in ('pending', 'committed', 'rolled_back', 'failed', 'imported')),
    confirm_deadline timestamptz,
    error            text not null default ''
);
create index config_revisions_state on config_revisions (state, id desc);

-- Кандидат один на систему, как в Junos: правки нескольких админов видны
-- друг другу, а base_revision ловит правку поверх чужого коммита.
create table candidate (
    id            int primary key default 1 check (id = 1),
    body          jsonb not null,
    base_revision bigint,
    updated_at    timestamptz not null default now(),
    updated_by    text not null default ''
);

create table audit_log (
    id          bigserial primary key,
    at          timestamptz not null default now(),
    username    text not null,
    action      text not null,
    target      text not null default '',
    details     jsonb,
    remote_addr text not null default ''
);
create index audit_log_at on audit_log (at desc);

create table events (
    id        bigserial primary key,
    at        timestamptz not null,
    type      text not null,
    action    text not null,
    proto     smallint not null,
    src       inet not null,
    dst       inet not null,
    sport     integer not null,
    dport     integer not null,
    from_zone text not null default '',
    to_zone   text not null default '',
    rule_id   text not null default '',
    iface     text not null default '',
    packets   bigint not null default 0,
    bytes     bigint not null default 0,
    count     integer not null default 1
);
create index events_at on events (at desc);
create index events_src on events (src, at desc);

create table traffic (
    at       timestamptz not null,
    iface    text not null,
    rx_bps   bigint not null,
    tx_bps   bigint not null,
    primary key (iface, at)
);

create table settings (
    key   text primary key,
    value jsonb not null
);
