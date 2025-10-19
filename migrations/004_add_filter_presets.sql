-- Migration: Add filter_presets table
-- This migration creates the filter_presets table for saving dashboard filter configurations

-- For PostgreSQL
DO $$
BEGIN
    -- Check if filter_presets table doesn't exist
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_name = 'filter_presets'
    ) THEN
        -- Create filter_presets table
        CREATE TABLE filter_presets (
            id VARCHAR(32) PRIMARY KEY,
            user_id VARCHAR(32) NOT NULL,
            name VARCHAR(255) NOT NULL,
            description TEXT,
            is_shared BOOLEAN DEFAULT false NOT NULL,
            is_default BOOLEAN DEFAULT false NOT NULL,
            filter_data JSONB NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT fk_filter_presets_user
                FOREIGN KEY (user_id)
                REFERENCES users(id)
                ON DELETE CASCADE
        );

        -- Create indexes for better query performance
        CREATE INDEX idx_filter_presets_user_id ON filter_presets(user_id);
        CREATE INDEX idx_filter_presets_is_shared ON filter_presets(is_shared);

        RAISE NOTICE 'Successfully created filter_presets table';
    ELSE
        RAISE NOTICE 'filter_presets table already exists';
    END IF;
END
$$;

-- For SQLite (alternative approach)
-- Uncomment the following if using SQLite:

/*
-- SQLite approach - will fail silently if table already exists
CREATE TABLE IF NOT EXISTS filter_presets (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_shared BOOLEAN DEFAULT 0 NOT NULL,
    is_default BOOLEAN DEFAULT 0 NOT NULL,
    filter_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_filter_presets_user_id ON filter_presets(user_id);
CREATE INDEX IF NOT EXISTS idx_filter_presets_is_shared ON filter_presets(is_shared);
*/
