DELETE FROM inventory_cache WHERE host_id IN (SELECT id FROM hosts WHERE transport='agent');
DELETE FROM host_observations WHERE host_id IN (SELECT id FROM hosts WHERE transport='agent');
DROP TABLE enrollment_tokens;
DROP TABLE agent_identities;
