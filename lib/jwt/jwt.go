package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Jwt struct {
	Key                 []byte
	TokenExpireDuration time.Duration
}

type UserClaims struct {
	UserId uint `json:"user_id"`
	jwt.RegisteredClaims
}

func NewJwt(key string, tokenExpireDuration time.Duration) *Jwt {
	return &Jwt{
		Key:                 []byte(key),
		TokenExpireDuration: tokenExpireDuration,
	}
}

func (s *Jwt) GenerateToken(userId uint) string {
	token, _ := s.GenerateTokenBefore(userId, time.Time{})
	return token
}

// GenerateTokenBefore returns the token and its signed expiration timestamp.
func (s *Jwt) GenerateTokenBefore(userId uint, limit time.Time) (string, int64) {
	if len(s.Key) == 0 {
		fmt.Println("jwt key is nil")
		return "", 0
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return "", 0
	}
	deadline := time.Now().Add(s.TokenExpireDuration)
	if !limit.IsZero() && limit.Before(deadline) {
		deadline = limit
	}
	expiresAt := jwt.NewNumericDate(deadline)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256,
		UserClaims{
			UserId: userId,
			RegisteredClaims: jwt.RegisteredClaims{
				ID:        id.String(),
				ExpiresAt: expiresAt,
			},
		})
	token, err := t.SignedString(s.Key)
	if err != nil {
		fmt.Printf("jwt token generate error: %v", err)
		return "", 0
	}
	return token, expiresAt.Unix()
}

func (s *Jwt) ParseToken(tokenString string) (uint, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		return s.Key, nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		return 0, err
	}
	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims.UserId, nil
	}
	return 0, err
}
