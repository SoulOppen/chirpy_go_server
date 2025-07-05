-- name: RefreshToken :one
INSERT INTO refresh_tokens (
    token,
    created_at,
    updated_at,
    user_id,
    expires_at,
    revoked_at
    )
VALUES (
    $1,  
    NOW(),              
    NOW(),              
    $2,
    $3,
    $4  
)
RETURNING *;
-- name: GetUserFromRefreshToken :one
SELECT user_id
FROM refresh_tokens
WHERE token=$1
AND revoked_at IS NULL
AND expires_at > NOW();
-- name: UpdateRefreshToken :one
UPDATE refresh_tokens
SET
  updated_at = NOW(),
  revoked_at = NOW()
WHERE token = $1
RETURNING *;