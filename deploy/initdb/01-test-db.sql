-- Separate database for integration tests (TEST_DATABASE_URL), so tests never touch dev data.
CREATE DATABASE jarvis_test OWNER jarvis;
