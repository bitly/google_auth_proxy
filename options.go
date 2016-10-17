package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/18F/hmacauth"
	"github.com/bitly/oauth2_proxy/backends"
	"github.com/bitly/oauth2_proxy/providers"
)

// Configuration Options that can be set by Command Line Flag, or Config File
type Options struct {
	ProxyPrefix  string `flag:"proxy-prefix" cfg:"proxy-prefix"`
	HttpAddress  string `flag:"http-address" cfg:"http_address"`
	HttpsAddress string `flag:"https-address" cfg:"https_address"`
	RedirectURL  string `flag:"redirect-url" cfg:"redirect_url"`
	ClientID     string `flag:"client-id" cfg:"client_id" env:"OAUTH2_PROXY_CLIENT_ID"`
	ClientSecret string `flag:"client-secret" cfg:"client_secret" env:"OAUTH2_PROXY_CLIENT_SECRET"`
	TLSCertFile  string `flag:"tls-cert" cfg:"tls_cert_file"`
	TLSKeyFile   string `flag:"tls-key" cfg:"tls_key_file"`

	AuthenticatedEmailsFile  string   `flag:"authenticated-emails-file" cfg:"authenticated_emails_file"`
	AzureTenant              string   `flag:"azure-tenant" cfg:"azure_tenant"`
	EmailDomains             []string `flag:"email-domain" cfg:"email_domains"`
	GitHubOrg                string   `flag:"github-org" cfg:"github_org"`
	GitHubTeam               string   `flag:"github-team" cfg:"github_team"`
	GoogleGroups             []string `flag:"google-group" cfg:"google_group"`
	GoogleAdminEmail         string   `flag:"google-admin-email" cfg:"google_admin_email"`
	GoogleServiceAccountJSON string   `flag:"google-service-account-json" cfg:"google_service_account_json"`
	HtpasswdFile             string   `flag:"htpasswd-file" cfg:"htpasswd_file"`
	DisplayHtpasswdForm      bool     `flag:"display-htpasswd-form" cfg:"display_htpasswd_form"`
	CustomTemplatesDir       string   `flag:"custom-templates-dir" cfg:"custom_templates_dir"`
	Footer                   string   `flag:"footer" cfg:"footer"`

	CookieName     string        `flag:"cookie-name" cfg:"cookie_name" env:"OAUTH2_PROXY_COOKIE_NAME"`
	CookieSecret   string        `flag:"cookie-secret" cfg:"cookie_secret" env:"OAUTH2_PROXY_COOKIE_SECRET"`
	CookieDomain   string        `flag:"cookie-domain" cfg:"cookie_domain" env:"OAUTH2_PROXY_COOKIE_DOMAIN"`
	CookieExpire   time.Duration `flag:"cookie-expire" cfg:"cookie_expire" env:"OAUTH2_PROXY_COOKIE_EXPIRE"`
	CookieRefresh  time.Duration `flag:"cookie-refresh" cfg:"cookie_refresh" env:"OAUTH2_PROXY_COOKIE_REFRESH"`
	CookieSecure   bool          `flag:"cookie-secure" cfg:"cookie_secure"`
	CookieHttpOnly bool          `flag:"cookie-httponly" cfg:"cookie_httponly"`

	Upstreams          []string `flag:"upstream" cfg:"upstreams"`
	SkipAuthRegex      []string `flag:"skip-auth-regex" cfg:"skip_auth_regex"`
	PassBasicAuth      bool     `flag:"pass-basic-auth" cfg:"pass_basic_auth"`
	BasicAuthPassword  string   `flag:"basic-auth-password" cfg:"basic_auth_password"`
	PassAccessToken    bool     `flag:"pass-access-token" cfg:"pass_access_token"`
	PassHostHeader     bool     `flag:"pass-host-header" cfg:"pass_host_header"`
	SkipProviderButton bool     `flag:"skip-provider-button" cfg:"skip_provider_button"`

	// These options allow for other providers besides Google, with
	// potential overrides.
	Provider          string `flag:"provider" cfg:"provider"`
	LoginURL          string `flag:"login-url" cfg:"login_url"`
	RedeemURL         string `flag:"redeem-url" cfg:"redeem_url"`
	ProfileURL        string `flag:"profile-url" cfg:"profile_url"`
	ProtectedResource string `flag:"resource" cfg:"resource"`
	ValidateURL       string `flag:"validate-url" cfg:"validate_url"`
	Scope             string `flag:"scope" cfg:"scope"`
	ApprovalPrompt    string `flag:"approval-prompt" cfg:"approval_prompt"`

	RequestLogging bool `flag:"request-logging" cfg:"request_logging"`

	SignatureKey string `flag:"signature-key" cfg:"signature_key" env:"OAUTH2_PROXY_SIGNATURE_KEY"`

	// These options allow for defining a list of aws upstream servers. Requests to these upstreams will be signed with
	// the provided key/secret provided as well. Env vars are read from the standard aws env var names.
	AwsUpstreams       []string `flag:"aws-upstream" cfg:"aws_upstreams"`
	AwsAccessKeyId     string   `flag:"aws-access-key-id" cfg"aws_access_key_id" env:"AWS_ACCESS_KEY"`
	AwsSecretAccessKey string   `flag:"aws-secret-access-key" cfg"aws_secret_access_key" env:"AWS_SECRET_KEY"`

	// internal values that are set after config validation
	redirectURL   *url.URL
	proxyURLs     []*proxyURL
	CompiledRegex []*regexp.Regexp
	provider      providers.Provider
	signatureData *backends.GAPSignatureData
}

type proxyURL struct {
	Url         *url.URL
	BackendType string
	Options     *backends.Options
}

func NewOptions() *Options {
	return &Options{
		ProxyPrefix:         "/oauth2",
		HttpAddress:         "127.0.0.1:4180",
		HttpsAddress:        ":443",
		DisplayHtpasswdForm: true,
		CookieName:          "_oauth2_proxy",
		CookieSecure:        true,
		CookieHttpOnly:      true,
		CookieExpire:        time.Duration(168) * time.Hour,
		CookieRefresh:       time.Duration(0),
		PassBasicAuth:       true,
		PassAccessToken:     false,
		PassHostHeader:      true,
		SkipProviderButton:  false,
		ApprovalPrompt:      "force",
		RequestLogging:      true,
	}
}

func parseURL(to_parse string, urltype string, msgs []string) (*url.URL, []string) {
	parsed, err := url.Parse(to_parse)
	if err != nil {
		return nil, append(msgs, fmt.Sprintf(
			"error parsing %s-url=%q %s", urltype, to_parse, err))
	}
	return parsed, msgs
}

func parseUpstreamUrl(u string, msgs []string) (*url.URL, []string) {
	upstreamURL, msgs := parseURL(u, "upstream", msgs)
	if upstreamURL.Path == "" {
		upstreamURL.Path = "/"
	}
	return upstreamURL, msgs
}

func (o *Options) Validate() error {
	msgs := make([]string, 0)
	if len(o.Upstreams) < 1 && len(o.AwsUpstreams) < 1 {
		msgs = append(msgs, "missing setting: at least one set of upstreams is required, either upstreams or aws-upstreams requires")
	}
	if o.CookieSecret == "" {
		msgs = append(msgs, "missing setting: cookie-secret")
	}
	if o.ClientID == "" {
		msgs = append(msgs, "missing setting: client-id")
	}
	if o.ClientSecret == "" {
		msgs = append(msgs, "missing setting: client-secret")
	}
	if o.AuthenticatedEmailsFile == "" && len(o.EmailDomains) == 0 && o.HtpasswdFile == "" {
		msgs = append(msgs, "missing setting for email validation: email-domain or authenticated-emails-file required.\n      use email-domain=* to authorize all email addresses")
	}

	o.redirectURL, msgs = parseURL(o.RedirectURL, "redirect", msgs)

	msgs = parseSignatureKey(o, msgs)

	for _, u := range o.Upstreams {
		var upstreamURL *url.URL
		upstreamURL, msgs = parseUpstreamUrl(u, msgs)
		o.proxyURLs = append(o.proxyURLs, &proxyURL{
			Url:         upstreamURL,
			BackendType: backends.BackendTypeDefault,
			Options: &backends.Options{
				SignatureData:  o.signatureData,
				PassHostHeader: o.PassHostHeader,
			},
		})
	}

	for _, u := range o.AwsUpstreams {
		var upstreamURL *url.URL
		upstreamURL, msgs = parseUpstreamUrl(u, msgs)
		o.proxyURLs = append(o.proxyURLs, &proxyURL{
			Url:         upstreamURL,
			BackendType: backends.BackendTypeAws,
			Options: &backends.Options{
				ClientKey:    o.AwsAccessKeyId,
				ClientSecret: o.AwsSecretAccessKey,
			},
		})
	}

	for _, u := range o.SkipAuthRegex {
		CompiledRegex, err := regexp.Compile(u)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf(
				"error compiling regex=%q %s", u, err))
		}
		o.CompiledRegex = append(o.CompiledRegex, CompiledRegex)
	}
	msgs = parseProviderInfo(o, msgs)

	if o.PassAccessToken || (o.CookieRefresh != time.Duration(0)) {
		valid_cookie_secret_size := false
		for _, i := range []int{16, 24, 32} {
			if len(secretBytes(o.CookieSecret)) == i {
				valid_cookie_secret_size = true
			}
		}
		var decoded bool
		if string(secretBytes(o.CookieSecret)) != o.CookieSecret {
			decoded = true
		}
		if valid_cookie_secret_size == false {
			var suffix string
			if decoded {
				suffix = fmt.Sprintf(" note: cookie secret was base64 decoded from %q", o.CookieSecret)
			}
			msgs = append(msgs, fmt.Sprintf(
				"cookie_secret must be 16, 24, or 32 bytes "+
					"to create an AES cipher when "+
					"pass_access_token == true or "+
					"cookie_refresh != 0, but is %d bytes.%s",
				len(secretBytes(o.CookieSecret)), suffix))
		}
	}

	if o.CookieRefresh >= o.CookieExpire {
		msgs = append(msgs, fmt.Sprintf(
			"cookie_refresh (%s) must be less than "+
				"cookie_expire (%s)",
			o.CookieRefresh.String(),
			o.CookieExpire.String()))
	}

	if len(o.GoogleGroups) > 0 || o.GoogleAdminEmail != "" || o.GoogleServiceAccountJSON != "" {
		if len(o.GoogleGroups) < 1 {
			msgs = append(msgs, "missing setting: google-group")
		}
		if o.GoogleAdminEmail == "" {
			msgs = append(msgs, "missing setting: google-admin-email")
		}
		if o.GoogleServiceAccountJSON == "" {
			msgs = append(msgs, "missing setting: google-service-account-json")
		}
	}

	msgs = validateCookieName(o, msgs)

	if len(msgs) != 0 {
		return fmt.Errorf("Invalid configuration:\n  %s",
			strings.Join(msgs, "\n  "))
	}
	return nil
}

func parseProviderInfo(o *Options, msgs []string) []string {
	p := &providers.ProviderData{
		Scope:          o.Scope,
		ClientID:       o.ClientID,
		ClientSecret:   o.ClientSecret,
		ApprovalPrompt: o.ApprovalPrompt,
	}
	p.LoginURL, msgs = parseURL(o.LoginURL, "login", msgs)
	p.RedeemURL, msgs = parseURL(o.RedeemURL, "redeem", msgs)
	p.ProfileURL, msgs = parseURL(o.ProfileURL, "profile", msgs)
	p.ValidateURL, msgs = parseURL(o.ValidateURL, "validate", msgs)
	p.ProtectedResource, msgs = parseURL(o.ProtectedResource, "resource", msgs)

	o.provider = providers.New(o.Provider, p)
	switch p := o.provider.(type) {
	case *providers.AzureProvider:
		p.Configure(o.AzureTenant)
	case *providers.GitHubProvider:
		p.SetOrgTeam(o.GitHubOrg, o.GitHubTeam)
	case *providers.GoogleProvider:
		if o.GoogleServiceAccountJSON != "" {
			file, err := os.Open(o.GoogleServiceAccountJSON)
			if err != nil {
				msgs = append(msgs, "invalid Google credentials file: "+o.GoogleServiceAccountJSON)
			} else {
				p.SetGroupRestriction(o.GoogleGroups, o.GoogleAdminEmail, file)
			}
		}
	}
	return msgs
}

func parseSignatureKey(o *Options, msgs []string) []string {
	if o.SignatureKey == "" {
		return msgs
	}

	components := strings.Split(o.SignatureKey, ":")
	if len(components) != 2 {
		return append(msgs, "invalid signature hash:key spec: "+
			o.SignatureKey)
	}

	algorithm, secretKey := components[0], components[1]
	if hash, err := hmacauth.DigestNameToCryptoHash(algorithm); err != nil {
		return append(msgs, "unsupported signature hash algorithm: "+
			o.SignatureKey)
	} else {
		o.signatureData = &backends.GAPSignatureData{hash, secretKey}
	}
	return msgs
}

func validateCookieName(o *Options, msgs []string) []string {
	cookie := &http.Cookie{Name: o.CookieName}
	if cookie.String() == "" {
		return append(msgs, fmt.Sprintf("invalid cookie name: %q", o.CookieName))
	}
	return msgs
}

func addPadding(secret string) string {
	padding := len(secret) % 4
	switch padding {
	case 1:
		return secret + "==="
	case 2:
		return secret + "=="
	case 3:
		return secret + "="
	default:
		return secret
	}
}

// secretBytes attempts to base64 decode the secret, if that fails it treats the secret as binary
func secretBytes(secret string) []byte {
	b, err := base64.URLEncoding.DecodeString(addPadding(secret))
	if err == nil {
		return []byte(addPadding(string(b)))
	}
	return []byte(secret)
}
