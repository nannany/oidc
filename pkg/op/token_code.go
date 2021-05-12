package op

import (
	"context"
	"errors"
	"net/http"

	"github.com/caos/oidc/pkg/oidc"
	"github.com/caos/oidc/pkg/utils"
)

//CodeExchange handles the OAuth 2.0 authorization_code grant, including
//parsing, validating, authorizing the client and finally exchanging the code for tokens
func CodeExchange(w http.ResponseWriter, r *http.Request, exchanger Exchanger) {
	tokenReq, err := ParseAccessTokenRequest(r, exchanger.Decoder())
	if err != nil {
		RequestError(w, r, err)
	}
	if tokenReq.Code == "" {
		RequestError(w, r, ErrInvalidRequest("code missing"))
		return
	}
	authReq, client, err := ValidateAccessTokenRequest(r.Context(), tokenReq, exchanger)
	if err != nil {
		RequestError(w, r, err)
		return
	}
	resp, err := CreateTokenResponse(r.Context(), authReq, client, exchanger, true, tokenReq.Code, "")
	if err != nil {
		RequestError(w, r, err)
		return
	}
	utils.MarshalJSON(w, resp)
}

//ParseAccessTokenRequest parsed the http request into a oidc.AccessTokenRequest
func ParseAccessTokenRequest(r *http.Request, decoder utils.Decoder) (*oidc.AccessTokenRequest, error) {
	request := new(oidc.AccessTokenRequest)
	err := ParseAuthenticatedTokenRequest(r, decoder, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}

//ValidateAccessTokenRequest validates the token request parameters including authorization check of the client
//and returns the previous created auth request corresponding to the auth code
func ValidateAccessTokenRequest(ctx context.Context, tokenReq *oidc.AccessTokenRequest, exchanger Exchanger) (AuthRequest, Client, error) {
	authReq, client, err := AuthorizeCodeClient(ctx, tokenReq, exchanger)
	if err != nil {
		return nil, nil, err
	}
	if client.GetID() != authReq.GetClientID() {
		return nil, nil, ErrInvalidRequest("invalid auth code")
	}
	if tokenReq.RedirectURI != authReq.GetRedirectURI() {
		return nil, nil, ErrInvalidRequest("redirect_uri does not correspond")
	}
	return authReq, client, nil
}

//AuthorizeCodeClient checks the authorization of the client and that the used method was the one previously registered.
//It than returns the auth request corresponding to the auth code
func AuthorizeCodeClient(ctx context.Context, tokenReq *oidc.AccessTokenRequest, exchanger Exchanger) (request AuthRequest, client Client, err error) {
	if tokenReq.ClientAssertionType == oidc.ClientAssertionTypeJWTAssertion {
		jwtExchanger, ok := exchanger.(JWTAuthorizationGrantExchanger)
		if !ok || !exchanger.AuthMethodPrivateKeyJWTSupported() {
			return nil, nil, errors.New("auth_method private_key_jwt not supported")
		}
		client, err = AuthorizePrivateJWTKey(ctx, tokenReq.ClientAssertion, jwtExchanger)
		if err != nil {
			return nil, nil, err
		}
		request, err = AuthRequestByCode(ctx, exchanger.Storage(), tokenReq.Code)
		return request, client, err
	}
	client, err = exchanger.Storage().GetClientByClientID(ctx, tokenReq.ClientID)
	if err != nil {
		return nil, nil, err
	}
	if client.AuthMethod() == oidc.AuthMethodPrivateKeyJWT {
		return nil, nil, errors.New("invalid_grant")
	}
	if client.AuthMethod() == oidc.AuthMethodNone {
		request, err = AuthRequestByCode(ctx, exchanger.Storage(), tokenReq.Code)
		if err != nil {
			return nil, nil, err
		}
		err = AuthorizeCodeChallenge(tokenReq, request.GetCodeChallenge())
		return request, client, err
	}
	if client.AuthMethod() == oidc.AuthMethodPost && !exchanger.AuthMethodPostSupported() {
		return nil, nil, errors.New("auth_method post not supported")
	}
	err = AuthorizeClientIDSecret(ctx, tokenReq.ClientID, tokenReq.ClientSecret, exchanger.Storage())
	request, err = AuthRequestByCode(ctx, exchanger.Storage(), tokenReq.Code)
	return request, client, err
}

//AuthRequestByCode returns the AuthRequest previously created from Storage corresponding to the auth code or an error
func AuthRequestByCode(ctx context.Context, storage Storage, code string) (AuthRequest, error) {
	authReq, err := storage.AuthRequestByCode(ctx, code)
	if err != nil {
		return nil, ErrInvalidRequest("invalid code")
	}
	return authReq, nil
}