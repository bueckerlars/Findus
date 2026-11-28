package authjwt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"findus/backend/internal/domain"
)

func TestSignParseRoundTrip(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	tok, err := Sign(secret, "user1", domain.RoleUser, 3, time.Hour)
	require.NoError(t, err)
	c, err := Parse(secret, tok)
	require.NoError(t, err)
	require.Equal(t, "user1", c.UserID)
	require.Equal(t, 3, c.SessionVersion)
	require.Equal(t, string(domain.RoleUser), c.Role)
}
