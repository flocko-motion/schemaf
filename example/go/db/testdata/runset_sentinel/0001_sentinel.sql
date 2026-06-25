-- This migration must NEVER be applied by RunSet: it belongs to a set that is
-- registered globally but not passed to RunSet. Its presence would prove RunSet
-- leaked into other registered sets.
CREATE TABLE IF NOT EXISTS runset_sentinel_marker (id INT);
