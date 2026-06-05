package sso

import (
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	saml2 "github.com/russellhaering/gosaml2"
	saml2types "github.com/russellhaering/gosaml2/types"
	dsig "github.com/russellhaering/goxmldsig"
)

// SAMLConfig is a SAML provider's settings (from identity_providers.config).
type SAMLConfig struct {
	IDPEntityID    string `json:"idp_entity_id"`
	IDPSSOURL      string `json:"idp_sso_url"`
	IDPCertificate string `json:"idp_certificate"` // PEM-encoded X.509
	SPEntityID     string `json:"sp_entity_id"`
}

// commonEmailAttrs are the SAML attribute names IdPs use for email.
var commonEmailAttrs = []string{
	"email", "mail", "emailaddress", "Email", "EmailAddress",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	"urn:oid:0.9.2342.19200300.100.1.3",
}
var commonNameAttrs = []string{
	"name", "displayName", "cn",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
}

// NewSAMLSP builds the gosaml2 service provider from config + this SP's ACS URL.
func NewSAMLSP(cfg SAMLConfig, acsURL string) (*saml2.SAMLServiceProvider, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(cfg.IDPCertificate)))
	if block == nil {
		return nil, fmt.Errorf("idp_certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse idp certificate: %w", err)
	}
	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	sp := &saml2.SAMLServiceProvider{
		IdentityProviderSSOURL:      cfg.IDPSSOURL,
		IdentityProviderIssuer:      cfg.IDPEntityID,
		AssertionConsumerServiceURL: acsURL,
		ServiceProviderIssuer:       cfg.SPEntityID,
		AudienceURI:                 cfg.SPEntityID,
		SignAuthnRequests:           false,
		IDPCertificateStore:         store,
	}
	return sp, nil
}

// SAMLMetadataXML builds this SP's SAML metadata document (EntityDescriptor
// with the ACS URL + SP entity id) so an IdP admin can register us by URL
// instead of hand-entering endpoints. We construct the descriptor directly
// rather than via gosaml2's Metadata(), which requires an SP signing/encryption
// keystore — we're an unsigned SP (we don't sign AuthnRequests).
func SAMLMetadataXML(cfg SAMLConfig, acsURL string) ([]byte, error) {
	entityID := cfg.SPEntityID
	if entityID == "" {
		entityID = acsURL
	}
	md := &saml2types.EntityDescriptor{
		ValidUntil: time.Now().UTC().Add(7 * 24 * time.Hour),
		EntityID:   entityID,
		SPSSODescriptor: &saml2types.SPSSODescriptor{
			AuthnRequestsSigned:        false,
			WantAssertionsSigned:       true,
			ProtocolSupportEnumeration: saml2.SAMLProtocolNamespace,
			AssertionConsumerServices: []saml2types.IndexedEndpoint{{
				Binding:  saml2.BindingHttpPost,
				Location: acsURL,
				Index:    1,
			}},
		},
	}
	out, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal SAML metadata: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// SAMLAuthRedirect returns the IdP redirect URL (HTTP-Redirect binding) carrying
// the AuthnRequest + relay state.
func SAMLAuthRedirect(sp *saml2.SAMLServiceProvider, relayState string) (string, error) {
	return sp.BuildAuthURL(relayState)
}

// VerifySAMLResponse validates an encoded SAMLResponse (signature against the
// IdP cert, plus time/audience/one-time conditions) and extracts the identity.
func VerifySAMLResponse(sp *saml2.SAMLServiceProvider, encodedResponse string) (Identity, error) {
	info, err := sp.RetrieveAssertionInfo(encodedResponse)
	if err != nil {
		return Identity{}, fmt.Errorf("validate SAML response: %w", err)
	}
	if w := info.WarningInfo; w != nil && (w.InvalidTime || w.NotInAudience) {
		return Identity{}, fmt.Errorf("SAML assertion failed conditions (time/audience)")
	}
	if info.NameID == "" {
		return Identity{}, fmt.Errorf("SAML assertion has no NameID")
	}
	id := Identity{Subject: info.NameID, Name: firstAttr(info.Values, commonNameAttrs)}
	id.Email = firstAttr(info.Values, commonEmailAttrs)
	if id.Email == "" && strings.Contains(info.NameID, "@") {
		id.Email = info.NameID // NameID is often an email
	}
	return id, nil
}

func firstAttr(vals saml2.Values, names []string) string {
	for _, n := range names {
		if a, ok := vals[n]; ok {
			if v := firstValue(a); v != "" {
				return v
			}
		}
	}
	return ""
}

func firstValue(a saml2types.Attribute) string {
	for _, v := range a.Values {
		if v.Value != "" {
			return v.Value
		}
	}
	return ""
}
