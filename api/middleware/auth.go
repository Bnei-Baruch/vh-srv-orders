package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

func TokenSource() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenSource := keycloak.AuthHeaderTokenSource(c.Request.Header.Get("Authorization"))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), common.CtxTokenSource, tokenSource))
		c.Next()
	}
}

type Roles struct {
	Roles []string `json:"roles"`
}

type IDTokenClaims struct {
	Acr               string           `json:"acr"`
	AllowedOrigins    []string         `json:"allowed-origins"`
	Aud               interface{}      `json:"aud"`
	AuthTime          int              `json:"auth_time"`
	Azp               string           `json:"azp"`
	Email             string           `json:"email"`
	Exp               int              `json:"exp"`
	FamilyName        string           `json:"family_name"`
	GivenName         string           `json:"given_name"`
	Iat               int              `json:"iat"`
	Iss               string           `json:"iss"`
	Jti               string           `json:"jti"`
	Name              string           `json:"name"`
	Nbf               int              `json:"nbf"`
	Nonce             string           `json:"nonce"`
	PreferredUsername string           `json:"preferred_username"`
	RealmAccess       Roles            `json:"realm_access"`
	ResourceAccess    map[string]Roles `json:"resource_access"`
	SessionState      string           `json:"session_state"`
	Sub               string           `json:"sub"`
	Typ               string           `json:"typ"`

	rolesMap map[string]struct{}
}

func (c *IDTokenClaims) initRoleMap() {
	if c.rolesMap == nil {
		c.rolesMap = make(map[string]struct{})
		if c.RealmAccess.Roles != nil {
			for _, r := range c.RealmAccess.Roles {
				c.rolesMap[r] = struct{}{}
			}
		}
	}
}

func (c *IDTokenClaims) HasAnyRole(roles ...string) bool {
	c.initRoleMap()
	for _, role := range roles {
		if _, ok := c.rolesMap[role]; ok {
			return true
		}
	}
	return false
}

type OIDCTokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

type FailoverOIDCTokenVerifier struct {
	verifiers []*oidc.IDTokenVerifier
}

func NewFailoverOIDCTokenVerifier(issuerUrls ...string) (OIDCTokenVerifier, error) {
	v := new(FailoverOIDCTokenVerifier)

	for _, url := range issuerUrls {
		provider, err := oidc.NewProvider(context.TODO(), url)
		if err != nil {
			return nil, fmt.Errorf("oidc.NewProvider: %w", err)
		}

		v.verifiers = append(v.verifiers, provider.Verifier(&oidc.Config{
			SkipClientIDCheck: true,
		}))
	}

	return v, nil
}

func (v *FailoverOIDCTokenVerifier) Verify(ctx context.Context, tokenStr string) (*oidc.IDToken, error) {
	var token *oidc.IDToken
	var err error

	for _, verifier := range v.verifiers {
		token, err = verifier.Verify(ctx, tokenStr)
		if err == nil {
			return token, nil
		}
	}

	return nil, err
}

func Authentication(tokenVerifier OIDCTokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := parseToken(c.Request)
		if auth == "" {
			c.Header("WWW-Authenticate", "Bearer realm=\"main\"")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		token, err := tokenVerifier.Verify(c.Request.Context(), auth)
		if err != nil {
			c.Header("WWW-Authenticate", fmt.Sprintf("Bearer realm=\"main\", error=\"invalid_token\" error_description=\"%s\"", err.Error()))
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		var claims IDTokenClaims
		if err := token.Claims(&claims); err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("malformed JWT claims: %w", err))
			return
		}

		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), common.CtxAuthClaims, &claims))

		c.Next()
	}
}

func parseToken(r *http.Request) string {
	authHeader := strings.Split(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if len(authHeader) == 2 &&
		strings.ToLower(authHeader[0]) == "bearer" &&
		len(authHeader[1]) > 0 {
		return authHeader[1]
	}
	return ""
}
