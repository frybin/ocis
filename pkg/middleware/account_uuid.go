package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	revauser "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/cs3org/reva/pkg/token/manager/jwt"
	mclient "github.com/micro/go-micro/v2/client"
	acc "github.com/owncloud/ocis-accounts/pkg/proto/v0"
	"github.com/owncloud/ocis-pkg/v2/log"
	ocisoidc "github.com/owncloud/ocis-pkg/v2/oidc"
	"github.com/owncloud/ocis-proxy/pkg/config"
)

// AccountMiddlewareOption defines a single option function.
type AccountMiddlewareOption func(o *AccountMiddlewareOptions)

// AccountMiddlewareOptions defines the available options for this package.
type AccountMiddlewareOptions struct {
	// Logger to use for logging, must be set
	Logger log.Logger
	// TokenManagerConfig for communicating with the reva token manager
	TokenManagerConfig config.TokenManager
}

// Logger provides a function to set the logger option.
func Logger(l log.Logger) AccountMiddlewareOption {
	return func(o *AccountMiddlewareOptions) {
		o.Logger = l
	}
}

// TokenManagerConfig provides a function to set the token manger config option.
func TokenManagerConfig(cfg config.TokenManager) AccountMiddlewareOption {
	return func(o *AccountMiddlewareOptions) {
		o.TokenManagerConfig = cfg
	}
}

func newAccountUUIDOptions(opts ...AccountMiddlewareOption) AccountMiddlewareOptions {
	opt := AccountMiddlewareOptions{}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

// AccountUUID provides a middleware which mints a jwt and adds it to the proxied request based
// on the oidc-claims
func AccountUUID(opts ...AccountMiddlewareOption) func(next http.Handler) http.Handler {
	opt := newAccountUUIDOptions(opts...)

	return func(next http.Handler) http.Handler {
		// TODO: handle error
		tokenManager, err := jwt.New(map[string]interface{}{
			"secret":  opt.TokenManagerConfig.JWTSecret,
			"expires": int64(60),
		})
		if err != nil {
			opt.Logger.Fatal().Err(err).Msgf("Could not initialize token-manager")
		}

		// TODO this won't work with a registry other than mdns. Look into Micro's client initialization.
		// https://github.com/owncloud/ocis-proxy/issues/38
		accounts := acc.NewAccountsService("com.owncloud.api.accounts", mclient.DefaultClient)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := opt.Logger
			claims, ok := r.Context().Value(ClaimsKey).(ocisoidc.StandardClaims)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// TODO allow lookup by username?
			// TODO allow lookup by custom claim, eg an id

			var uuid string
			entry, err := svcCache.Get(AccountsKey, claims.Email)
			if err != nil {
				l.Debug().Msgf("No cache entry for %v", claims.Email)
				resp, err := accounts.ListAccounts(context.Background(), &acc.ListAccountsRequest{
					Query:    fmt.Sprintf("mail eq '%s'", claims.Email), // TODO encode mail
					PageSize: 2,
				})

				if err != nil {
					l.Error().Err(err).Str("email", claims.Email).Msgf("Error fetching from accounts-service")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				if len(resp.Accounts) <= 0 {
					l.Error().Str("email", claims.Email).Msgf("Account not found")
					w.WriteHeader(http.StatusNotFound)
					return
				}

				if len(resp.Accounts) > 1 {
					l.Error().Str("email", claims.Email).Msgf("More than one account with this email found. Not logging user in.")
					w.WriteHeader(http.StatusForbidden)
					return
				}

				err = svcCache.Set(AccountsKey, claims.Email, resp.Accounts[0].Id)
				if err != nil {
					l.Err(err).Str("email", claims.Email).Msgf("Could not cache user")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				uuid = resp.Accounts[0].Id
			} else {
				uuid, ok = entry.V.(string)
				if !ok {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}

			l.Debug().Interface("claims", claims).Interface("uuid", uuid).Msgf("Associated claims with uuid")
			token, err := tokenManager.MintToken(r.Context(), &revauser.User{
				Id: &revauser.UserId{
					OpaqueId: uuid,
				},
				Username:     strings.ToLower(claims.Name),
				DisplayName:  claims.Name,
				Mail:         claims.Email,
				MailVerified: claims.EmailVerified,
				// TODO groups
			})

			if err != nil {
				l.Error().Err(err).Msgf("Could not mint token")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			r.Header.Set("x-access-token", token)
			next.ServeHTTP(w, r)
		})
	}
}
