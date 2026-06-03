package sso

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// buildSignedResponse mints a self-signed IdP cert and returns (certPEM,
// base64-encoded signed SAMLResponse) for the given subject/audience/acs.
func buildSignedResponse(t *testing.T, email, audience, acs string) (string, string) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	now := time.Now().UTC()
	iso := "2006-01-02T15:04:05Z"
	notBefore := now.Add(-time.Minute).Format(iso)
	notAfter := now.Add(5 * time.Minute).Format(iso)

	// Build a self-contained, signable Assertion (declares its own namespace) and
	// sign it enveloped. gosaml2 verifies an assertion-level signature.
	assertion := etree.NewElement("Assertion")
	assertion.Space = "saml"
	assertion.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	assertion.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	assertion.CreateAttr("ID", "_assertion1")
	assertion.CreateAttr("Version", "2.0")
	assertion.CreateAttr("IssueInstant", now.Format(iso))
	assertion.CreateElement("saml:Issuer").SetText("test-idp")

	subj := assertion.CreateElement("saml:Subject")
	nameID := subj.CreateElement("saml:NameID")
	nameID.CreateAttr("Format", "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress")
	nameID.SetText(email)
	sc := subj.CreateElement("saml:SubjectConfirmation")
	sc.CreateAttr("Method", "urn:oasis:names:tc:SAML:2.0:cm:bearer")
	scd := sc.CreateElement("saml:SubjectConfirmationData")
	scd.CreateAttr("NotOnOrAfter", notAfter)
	scd.CreateAttr("Recipient", acs)

	cond := assertion.CreateElement("saml:Conditions")
	cond.CreateAttr("NotBefore", notBefore)
	cond.CreateAttr("NotOnOrAfter", notAfter)
	cond.CreateElement("saml:AudienceRestriction").CreateElement("saml:Audience").SetText(audience)

	attrStmt := assertion.CreateElement("saml:AttributeStatement")
	emailAttr := attrStmt.CreateElement("saml:Attribute")
	emailAttr.CreateAttr("Name", "email")
	emailAttr.CreateElement("saml:AttributeValue").SetText(email)

	ctx := dsig.NewDefaultSigningContext(memKeyStore{key: key, certDER: certDER})
	signedAssertion, err := ctx.SignEnveloped(assertion)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Serialize the signed assertion standalone, then wrap in a Response. The
	// assertion keeps its own namespace declarations, so its canonical form is
	// stable across the move (no re-parenting of an unsigned element).
	adoc := etree.NewDocument()
	adoc.SetRoot(signedAssertion)
	assertionXML, err := adoc.WriteToString()
	if err != nil {
		t.Fatalf("serialize assertion: %v", err)
	}
	respXML := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_response1" Version="2.0" IssueInstant="` + now.Format(iso) + `" Destination="` + acs + `">` +
		`<saml:Issuer>test-idp</saml:Issuer>` +
		`<samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>` +
		stripXMLDecl(assertionXML) +
		`</samlp:Response>`
	return certPEM, base64.StdEncoding.EncodeToString([]byte(respXML))
}

// memKeyStore is a goxmldsig X509KeyStore backed by an in-memory key+cert.
type memKeyStore struct {
	key     *rsa.PrivateKey
	certDER []byte
}

func (m memKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return m.key, m.certDER, nil }

func TestVerifySAMLResponseHappy(t *testing.T) {
	acs := "https://sp.test/acs"
	certPEM, encoded := buildSignedResponse(t, "user@corp.test", "teggo-sp", acs)
	sp, err := NewSAMLSP(SAMLConfig{
		IDPEntityID: "test-idp", IDPSSOURL: "https://idp.test/sso",
		IDPCertificate: certPEM, SPEntityID: "teggo-sp",
	}, acs)
	if err != nil {
		t.Fatalf("sp: %v", err)
	}
	id, err := VerifySAMLResponse(sp, encoded)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "user@corp.test" || id.Email != "user@corp.test" {
		t.Errorf("identity = %+v", id)
	}
}

func TestVerifySAMLResponseRejectsTampered(t *testing.T) {
	acs := "https://sp.test/acs"
	certPEM, encoded := buildSignedResponse(t, "user@corp.test", "teggo-sp", acs)
	sp, _ := NewSAMLSP(SAMLConfig{IDPEntityID: "test-idp", IDPSSOURL: "https://idp.test/sso", IDPCertificate: certPEM, SPEntityID: "teggo-sp"}, acs)

	// Flip a byte in the (decoded) XML → signature must fail.
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw = []byte(string(raw) + "<!-- tamper -->")
	tampered := base64.StdEncoding.EncodeToString([]byte(replaceFirst(string(raw), "user@corp.test", "evil@attacker.test")))
	if _, err := VerifySAMLResponse(sp, tampered); err == nil {
		t.Error("tampered assertion must fail verification")
	}

	// Garbage input is rejected.
	if _, err := VerifySAMLResponse(sp, base64.StdEncoding.EncodeToString([]byte("<not-saml/>"))); err == nil {
		t.Error("garbage response must fail")
	}
}

func TestVerifySAMLResponseWrongAudience(t *testing.T) {
	acs := "https://sp.test/acs"
	certPEM, encoded := buildSignedResponse(t, "user@corp.test", "some-other-sp", acs)
	sp, _ := NewSAMLSP(SAMLConfig{IDPEntityID: "test-idp", IDPSSOURL: "https://idp.test/sso", IDPCertificate: certPEM, SPEntityID: "teggo-sp"}, acs)
	if _, err := VerifySAMLResponse(sp, encoded); err == nil {
		t.Error("assertion for a different audience must be rejected")
	}
}

func stripXMLDecl(s string) string {
	if i := indexOf(s, "?>"); i >= 0 {
		return s[i+2:]
	}
	return s
}

func replaceFirst(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
