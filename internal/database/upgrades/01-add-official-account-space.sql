-- v2: Add official account space room
ALTER TABLE "user" ADD COLUMN official_account_space_room TEXT;
UPDATE "user" SET official_account_space_room = '' WHERE official_account_space_room IS NULL;

ALTER TABLE "user_portal" ADD COLUMN in_official_account_space BOOLEAN NOT NULL DEFAULT false;
UPDATE "user_portal" SET in_official_account_space = false WHERE in_official_account_space IS NULL;
