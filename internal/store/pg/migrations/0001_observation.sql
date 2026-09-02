-- What a model said about one moment of a film.
--
-- The primary key is the point. Analysis is resumed, retried and re-run, and
-- observations that stacked rather than replaced once turned 459 distinct
-- moments into 3720 rows and needed a repair script written by hand. Here that
-- is a conflict, and the conflict has an answer.
--
-- `at` is film time, in seconds, and never chunk time. A chunk starting an hour
-- in that reports its own clock files every observation under the opening
-- minutes; this project has had that bug, and it took a comparison against the
-- scores to notice.
create table observation (
    film     text not null,
    at       double precision not null,
    place    text not null default '',
    doing    text not null default '',
    seen     text not null default '',
    labels   text[] not null default '{}',
    built_at timestamptz not null default now(),
    primary key (film, at)
);

-- Everything that reads these walks a film forwards.
create index observation_by_time on observation (film, at);
