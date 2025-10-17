-- Migration: Update UserSentryConfig.UserID from uint to varchar(32)
-- This migration safely converts the UserID column type while preserving data compatibility

-- For PostgreSQL
DO $$
BEGIN
    -- Check if table exists and has the old numeric user_id column
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'user_sentry_configs' 
        AND column_name = 'user_id' 
        AND data_type IN ('integer', 'bigint')
    ) THEN
        -- First, add a temporary column for the new string user_id
        ALTER TABLE user_sentry_configs ADD COLUMN user_id_new VARCHAR(32);
        
        -- Convert existing numeric user IDs to string format
        -- Note: This assumes user IDs are being converted from numeric to string format
        UPDATE user_sentry_configs SET user_id_new = CAST(user_id AS VARCHAR(32));
        
        -- Drop the old column and rename the new one
        ALTER TABLE user_sentry_configs DROP COLUMN user_id;
        ALTER TABLE user_sentry_configs RENAME COLUMN user_id_new TO user_id;
        
        -- Add constraints
        ALTER TABLE user_sentry_configs ALTER COLUMN user_id SET NOT NULL;
        CREATE UNIQUE INDEX idx_user_sentry_configs_user_id ON user_sentry_configs(user_id);
        
        RAISE NOTICE 'Successfully migrated user_sentry_configs.user_id to VARCHAR(32)';
    ELSE
        RAISE NOTICE 'user_sentry_configs.user_id is already VARCHAR type or table does not exist';
    END IF;
END
$$;

-- For SQLite (alternative approach)
-- SQLite doesn't support ALTER COLUMN, so we need to recreate the table
-- Uncomment the following if using SQLite:

/*
-- Check if table exists first
-- CREATE TABLE IF NOT EXISTS user_sentry_configs_new (
--     id INTEGER PRIMARY KEY AUTOINCREMENT,
--     created_at DATETIME,
--     updated_at DATETIME,
--     deleted_at DATETIME,
--     user_id VARCHAR(32) NOT NULL,
--     personal_token TEXT,
--     sentry_base_url VARCHAR(255) DEFAULT 'https://sentry.io'
-- );
-- 
-- -- Copy data from old table (converting user_id to string)
-- INSERT INTO user_sentry_configs_new (id, created_at, updated_at, deleted_at, user_id, personal_token, sentry_base_url)
-- SELECT id, created_at, updated_at, deleted_at, CAST(user_id AS VARCHAR(32)), personal_token, sentry_base_url
-- FROM user_sentry_configs
-- WHERE EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='user_sentry_configs');
-- 
-- -- Drop old table and rename new one
-- DROP TABLE IF EXISTS user_sentry_configs;
-- ALTER TABLE user_sentry_configs_new RENAME TO user_sentry_configs;
-- 
-- -- Create unique index
-- CREATE UNIQUE INDEX idx_user_sentry_configs_user_id ON user_sentry_configs(user_id);
*/