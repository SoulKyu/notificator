-- Migration: Add sound_notifications_enabled to notification_preferences table
-- This migration adds the sound_notifications_enabled column to enable/disable notification sounds

-- For PostgreSQL
DO $$
BEGIN
    -- Check if notification_preferences table exists
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_name = 'notification_preferences'
    ) THEN
        -- Check if column doesn't already exist
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'notification_preferences'
            AND column_name = 'sound_notifications_enabled'
        ) THEN
            -- Add the sound_notifications_enabled column with default value true
            ALTER TABLE notification_preferences
            ADD COLUMN sound_notifications_enabled BOOLEAN DEFAULT true NOT NULL;

            RAISE NOTICE 'Successfully added sound_notifications_enabled column to notification_preferences table';
        ELSE
            RAISE NOTICE 'sound_notifications_enabled column already exists in notification_preferences table';
        END IF;
    ELSE
        RAISE NOTICE 'notification_preferences table does not exist yet, will be created by AutoMigrate';
    END IF;
END
$$;

-- For SQLite (alternative approach)
-- Uncomment the following if using SQLite:

/*
-- SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we need a different approach
-- This will fail silently if the column already exists or table doesn't exist

-- Check if table exists and add column
-- Note: SQLite will throw an error if column exists, but won't break the database
ALTER TABLE notification_preferences
ADD COLUMN sound_notifications_enabled BOOLEAN DEFAULT 1 NOT NULL;
*/
