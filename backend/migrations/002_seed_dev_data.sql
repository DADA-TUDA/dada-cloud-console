-- 002_seed_dev_data.sql
-- Dev seed data (only inserted in DEV_MODE)
-- Password "admin" bcrypt hash (cost=10, generated with bcrypt.GenerateFromPassword)

INSERT INTO users (id, username, email, password_hash, display_name)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'admin',  'admin@dada.local',  '$2a$10$fsjxCKDrETpkKU9vVLLFPeJplStPa3MkHgTAUKxOvFG/CWbZWqkrC', 'Platform Admin'),
    ('00000000-0000-0000-0000-000000000002', 'alex',   'alex@dada.local',   '$2a$10$fsjxCKDrETpkKU9vVLLFPeJplStPa3MkHgTAUKxOvFG/CWbZWqkrC', 'Alex Developer'),
    ('00000000-0000-0000-0000-000000000003', 'client', 'client@dada.local', '$2a$10$fsjxCKDrETpkKU9vVLLFPeJplStPa3MkHgTAUKxOvFG/CWbZWqkrC', 'Client User')
ON CONFLICT (username) DO NOTHING;

INSERT INTO projects (id, name, display_name, owner_type)
VALUES
    ('10000000-0000-0000-0000-000000000001', 'internal', 'DADA Internal', 'team'),
    ('10000000-0000-0000-0000-000000000002', 'client-a', 'Client A Corp',  'client')
ON CONFLICT (name) DO NOTHING;

INSERT INTO environments (id, project_id, name, namespace, type)
VALUES
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'dev',  'internal-dev',  'dev'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'prod', 'internal-prod', 'prod'),
    ('20000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000002', 'prod', 'client-a-prod', 'prod')
ON CONFLICT (project_id, name) DO NOTHING;

INSERT INTO project_members (project_id, user_id, role)
VALUES
    ('10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'platform-admin'),
    ('10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'developer'),
    ('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'platform-admin'),
    ('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003', 'client-admin')
ON CONFLICT (project_id, user_id) DO NOTHING;
