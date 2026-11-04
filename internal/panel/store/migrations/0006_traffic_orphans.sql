-- User ids can be reused after a delete, so the daily history of deleted
-- users is kept only in the server total (user_id 0), never under an id a
-- new user may get.
INSERT INTO traffic_daily (user_id, day, bytes)
    SELECT 0, day, SUM(bytes) FROM traffic_daily
    WHERE user_id != 0 AND user_id NOT IN (SELECT id FROM users)
    GROUP BY day
    ON CONFLICT (user_id, day) DO UPDATE SET bytes = bytes + excluded.bytes;
DELETE FROM traffic_daily WHERE user_id != 0 AND user_id NOT IN (SELECT id FROM users);
