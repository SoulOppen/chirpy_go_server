-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email,hashed_password)
VALUES (
    gen_random_uuid(),  
    NOW(),              
    NOW(),              
    $1,
    $2  
)
RETURNING *;
-- name: ReturnHashPassword :one
SELECT hashed_password 
FROM users 
WHERE email=$1;
-- name: ReturnUserNotPassword :one
SELECT id,created_at, updated_at, email,is_chirpy_red
FROM users 
WHERE email=$1;
-- name: UpdateUser :one
UPDATE users 
SET
    updated_at = NOW(),
    email=$1,
    hashed_password=$2
WHERE id = $3
RETURNING *;
-- name: UpdateUserIsRed :one
UPDATE users 
SET
    updated_at = NOW(),
    is_chirpy_red= true
WHERE id = $1
RETURNING *;
