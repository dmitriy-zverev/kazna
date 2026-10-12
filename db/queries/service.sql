-- Connectivity probe used by /health/ready. Returns a constant so it works
-- before any domain tables exist.
-- name: Ping :one
SELECT 1 AS one;
