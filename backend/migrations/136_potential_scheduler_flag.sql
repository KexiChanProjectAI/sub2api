INSERT INTO settings (key, value)
VALUES
    ('openai_potential_scheduler_enabled', 'false')
ON CONFLICT (key) DO NOTHING;