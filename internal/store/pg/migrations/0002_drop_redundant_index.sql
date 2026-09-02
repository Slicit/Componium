-- The index 0001 created is the primary key spelled a second time.
--
-- (film, at) is already a unique btree because it is the primary key, so
-- observation_by_time duplicated it exactly: two identical trees maintained on
-- every insert, for one that was ever consulted.
--
-- Left as its own migration rather than edited into 0001, because 0001 has
-- already run somewhere and a migration that changes after it has been applied
-- is a migration nobody can reason about.
drop index if exists observation_by_time;
