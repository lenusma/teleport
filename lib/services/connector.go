package services

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/gravitational/teleport/lib/utils"

	"github.com/coreos/go-oidc/jose"
	"github.com/gravitational/trace"
)

// OIDCConnector specifies configuration for Open ID Connect compatible external
// identity provider, e.g. google in some organisation
type OIDCConnector interface {
	// Name is a provider name, 'e.g.' google, used internally
	GetName() string
	// Issuer URL is the endpoint of the provider, e.g. https://accounts.google.com
	GetIssuerURL() string
	// ClientID is id for authentication client (in our case it's our Auth server)
	GetClientID() string
	// ClientSecret is used to authenticate our client and should not
	// be visible to end user
	GetClientSecret() string
	// RedirectURL - Identity provider will use this URL to redirect
	// client's browser back to it after successfull authentication
	// Should match the URL on Provider's side
	GetRedirectURL() string
	// Display - Friendly name for this provider.
	GetDisplay() string
	// Scope is additional scopes set by provder
	GetScope() []string
	// ClaimsToRoles specifies dynamic mapping from claims to roles
	GetClaimsToRoles() []ClaimMapping
	// GetClaims returns list of claims expected by mappings
	GetClaims() []string
	// MapClaims maps claims to roles
	MapClaims(claims jose.Claims) []string
	// Check checks OIDC connector for errors
	Check() error
	// SetClientSecret sets client secret to some value
	SetClientSecret(secret string)
}

var connectorMarshaler OIDCConnectorMarshaler = &TeleportOIDCConnectorMarshaler{}

// SetOIDCConnectorMarshaler sets global user marshaler
func SetOIDCConnectorMarshaler(m OIDCConnectorMarshaler) {
	marshalerMutex.Lock()
	defer marshalerMutex.Unlock()
	connectorMarshaler = m
}

// GetOIDCConnectorMarshaler returns currently set user marshaler
func GetOIDCConnectorMarshaler() OIDCConnectorMarshaler {
	marshalerMutex.RLock()
	defer marshalerMutex.RUnlock()
	return connectorMarshaler
}

// OIDCConnectorMarshaler implements marshal/unmarshal of User implementations
// mostly adds support for extended versions
type OIDCConnectorMarshaler interface {
	// UnmarshalOIDCConnector unmarshals connector from binary representation
	UnmarshalOIDCConnector(bytes []byte) (OIDCConnector, error)
	// MarshalOIDCConnector marshals connector to binary representation
	MarshalOIDCConnector(c OIDCConnector) ([]byte, error)
}

// GetOIDCConnectorSchema returns schema for OIDCConnector
func GetOIDCConnectorSchema() string {
	return fmt.Sprintf(OIDCConnectorV1SchemaTemplate, MetadataSchema, OIDCConnectorSpecV1Schema)
}

type TeleportOIDCConnectorMarshaler struct{}

// UnmarshalOIDCConnector unmarshals connector from
func (*TeleportOIDCConnectorMarshaler) UnmarshalOIDCConnector(bytes []byte) (OIDCConnector, error) {
	var h ResourceHeader
	err := json.Unmarshal(bytes, &h)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	switch h.Version {
	case "":
		var c OIDCConnectorV0
		err := json.Unmarshal(bytes, &c)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		return c.V1(), nil
	case V1:
		var c OIDCConnectorV1
		if err := utils.UnmarshalWithSchema(GetOIDCConnectorSchema(), &c, bytes); err != nil {
			return nil, trace.BadParameter(err.Error())
		}
	}

	return nil, trace.BadParameter("OIDC connector resource version %v is not supported", h.Version)
}

// MarshalUser marshalls user into JSON
func (*TeleportOIDCConnectorMarshaler) MarshalOIDCConnector(c OIDCConnector) ([]byte, error) {
	return json.Marshal(c)
}

// OIDCConnectorV1 is version 1 resource spec for OIDC connector
type OIDCConnectorV1 struct {
	// Kind is a resource kind
	Kind string `json:"kind"`
	// Version is version
	Version string `json:"version"`
	// Metadata is connector metadata
	Metadata Metadata `json:"metadata"`
	// Spec contains connector specification
	Spec OIDCConnectorSpecV1 `json:"spec"`
}

// SetClientSecret sets client secret to some value
func (o *OIDCConnectorV1) SetClientSecret(secret string) {
	o.Spec.ClientSecret = secret
}

// ID is a provider id, 'e.g.' google, used internally
func (o *OIDCConnectorV1) GetName() string {
	return o.Metadata.Name
}

// Issuer URL is the endpoint of the provider, e.g. https://accounts.google.com
func (o *OIDCConnectorV1) GetIssuerURL() string {
	return o.Spec.IssuerURL
}

// ClientID is id for authentication client (in our case it's our Auth server)
func (o *OIDCConnectorV1) GetClientID() string {
	return o.Spec.ClientID
}

// ClientSecret is used to authenticate our client and should not
// be visible to end user
func (o *OIDCConnectorV1) GetClientSecret() string {
	return o.Spec.ClientSecret
}

// RedirectURL - Identity provider will use this URL to redirect
// client's browser back to it after successfull authentication
// Should match the URL on Provider's side
func (o *OIDCConnectorV1) GetRedirectURL() string {
	return o.Spec.RedirectURL
}

// Display - Friendly name for this provider.
func (o *OIDCConnectorV1) GetDisplay() string {
	return o.Spec.Display
}

// Scope is additional scopes set by provder
func (o *OIDCConnectorV1) GetScope() []string {
	return o.Spec.Scope
}

// ClaimsToRoles specifies dynamic mapping from claims to roles
func (o *OIDCConnectorV1) GetClaimsToRoles() []ClaimMapping {
	return o.Spec.ClaimsToRoles
}

// GetClaims returns list of claims expected by mappings
func (o *OIDCConnectorV1) GetClaims() []string {
	var out []string
	for _, mapping := range o.Spec.ClaimsToRoles {
		out = append(out, mapping.Claim)
	}
	return utils.Deduplicate(out)
}

// MapClaims maps claims to roles
func (o *OIDCConnectorV1) MapClaims(claims jose.Claims) []string {
	var roles []string
	for _, mapping := range o.Spec.ClaimsToRoles {
		for claimName := range claims {
			if claimName != mapping.Claim {
				continue
			}
			claimValue, ok, _ := claims.StringClaim(claimName)
			if ok && claimValue == mapping.Value {
				roles = append(roles, mapping.Roles...)
			}
			claimValues, ok, _ := claims.StringsClaim(claimName)
			if ok {
				for _, claimValue := range claimValues {
					if claimValue == mapping.Value {
						roles = append(roles, mapping.Roles...)
					}
				}
			}
		}
	}
	return utils.Deduplicate(roles)
}

// Check returns nil if all parameters are great, err otherwise
func (o *OIDCConnectorV1) Check() error {
	if o.Metadata.Name == "" {
		return trace.BadParameter("ID: missing connector name")
	}
	if _, err := url.Parse(o.Spec.IssuerURL); err != nil {
		return trace.BadParameter("IssuerURL: bad url: '%v'", o.Spec.IssuerURL)
	}
	if _, err := url.Parse(o.Spec.RedirectURL); err != nil {
		return trace.BadParameter("RedirectURL: bad url: '%v'", o.Spec.RedirectURL)
	}
	if o.Spec.ClientID == "" {
		return trace.BadParameter("ClientID: missing client id")
	}
	if o.Spec.ClientSecret == "" {
		return trace.BadParameter("ClientSecret: missing client secret")
	}
	return nil
}

// OIDCConnectorV1SchemaTemplate is a template JSON Schema for user
const OIDCConnectorV1SchemaTemplate = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["kind", "spec", "metadata", "version"],
  "properties": {
    "kind": {"type": "string"},
    "version": {"type": "string", "default": "v1"},
    "metadata": %v,
    "spec": %v
  }
}`

// OIDCConnectorSpecV1 specifies configuration for Open ID Connect compatible external
// identity provider, e.g. google in some organisation
type OIDCConnectorSpecV1 struct {
	// ID is a provider id, 'e.g.' google, used internally
	ID string `json:"id"`
	// Issuer URL is the endpoint of the provider, e.g. https://accounts.google.com
	IssuerURL string `json:"issuer_url"`
	// ClientID is id for authentication client (in our case it's our Auth server)
	ClientID string `json:"client_id"`
	// ClientSecret is used to authenticate our client and should not
	// be visible to end user
	ClientSecret string `json:"client_secret"`
	// RedirectURL - Identity provider will use this URL to redirect
	// client's browser back to it after successfull authentication
	// Should match the URL on Provider's side
	RedirectURL string `json:"redirect_url"`
	// Display - Friendly name for this provider.
	Display string `json:"display"`
	// Scope is additional scopes set by provder
	Scope []string `json:"scope"`
	// ClaimsToRoles specifies dynamic mapping from claims to roles
	ClaimsToRoles []ClaimMapping `json:"claims_to_roles"`
}

// OIDCConnectorSpecV1Schema is a JSON Schema for OIDC Connector
var OIDCConnectorSpecV1Schema = fmt.Sprintf(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["id", "issuer_url", "client_id", "client_secret", "redirect_url"],
  "properties": {
    "id": {"type": "string"},
    "issuer_url": {"type": "string"},
    "client_id": {"type": "string"},
    "client_secret": {"type": "string"},
    "redirect_url": {"type": "string"},
    "scope": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "claims_to_roles": {
      "type": "array",
      "items": %v
    }
  }
}`, ClaimMappingSchema)

// GetClaimNames returns a list of claim names from the claim values
func GetClaimNames(claims jose.Claims) []string {
	var out []string
	for claim := range claims {
		out = append(out, claim)
	}
	return out
}

// ClaimMapping is OIDC claim mapping that maps
// claim name to teleport roles
type ClaimMapping struct {
	// Claim is OIDC claim name
	Claim string `json:"claim"`
	// Value is claim value to match
	Value string `json:"value"`
	// Roles is a list of teleport roles to match
	Roles []string `json:"roles"`
}

// ClaimMappingSchema is JSON schema for claim mapping
const ClaimMappingSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["claim", "value", "roles"],
  "properties": {
     "claim": {"type": "string"}, 
     "value": {"type": "string"},
     "roles": {
        "type": "array",
        "items": {
          "type": "string"
        }
      }
   }
}`

// OIDCConnectorV0 specifies configuration for Open ID Connect compatible external
// identity provider, e.g. google in some organisation
type OIDCConnectorV0 struct {
	// ID is a provider id, 'e.g.' google, used internally
	ID string `json:"id"`
	// Issuer URL is the endpoint of the provider, e.g. https://accounts.google.com
	IssuerURL string `json:"issuer_url"`
	// ClientID is id for authentication client (in our case it's our Auth server)
	ClientID string `json:"client_id"`
	// ClientSecret is used to authenticate our client and should not
	// be visible to end user
	ClientSecret string `json:"client_secret"`
	// RedirectURL - Identity provider will use this URL to redirect
	// client's browser back to it after successfull authentication
	// Should match the URL on Provider's side
	RedirectURL string `json:"redirect_url"`
	// Display - Friendly name for this provider.
	Display string `json:"display"`
	// Scope is additional scopes set by provder
	Scope []string `json:"scope"`
	// ClaimsToRoles specifies dynamic mapping from claims to roles
	ClaimsToRoles []ClaimMapping `json:"claims_to_roles"`
}

// V1 returns V1 version of the connector
func (o OIDCConnectorV0) V1() *OIDCConnectorV1 {
	return &OIDCConnectorV1{
		Kind:    KindOIDCConnector,
		Version: V1,
		Metadata: Metadata{
			Name: o.ID,
		},
		Spec: OIDCConnectorSpecV1{
			IssuerURL:     o.IssuerURL,
			ClientID:      o.ClientID,
			ClientSecret:  o.ClientSecret,
			RedirectURL:   o.RedirectURL,
			Display:       o.Display,
			Scope:         o.Scope,
			ClaimsToRoles: o.ClaimsToRoles,
		},
	}
}
