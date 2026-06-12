package tenant

import (
	"strings"
	"testing"
	"time"
)

func valid() Input {
	return Input{
		OrgName:   "Acme Industrial",
		FullName:  "Ada Admin",
		Email:     "Ada@Acme.Test",
		Password:  "pw-123456",
		Subdomain: "acme",
	}
}

func TestValidateNormalizesAndDefaults(t *testing.T) {
	in := valid()
	in.Subdomain = "  ACME "
	if err := Validate(&in); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if in.Email != "ada@acme.test" {
		t.Errorf("email not lowered: %q", in.Email)
	}
	if in.Subdomain != "acme" {
		t.Errorf("subdomain not normalized: %q", in.Subdomain)
	}
	if in.Currency != "USD" || in.Locale != "en" || in.BaseDomain != "teggo.local" {
		t.Errorf("defaults: currency=%q locale=%q base=%q", in.Currency, in.Locale, in.BaseDomain)
	}
	if in.VerifyTTL != 48*time.Hour {
		t.Errorf("verify ttl default: %v", in.VerifyTTL)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Input)
		want string
	}{
		{"empty org", func(i *Input) { i.OrgName = "  " }, "organization name"},
		{"empty name", func(i *Input) { i.FullName = "" }, "your name"},
		{"bad email", func(i *Input) { i.Email = "not-an-email" }, "email"},
		{"short password", func(i *Input) { i.Password = "short" }, "at least 8"},
		{"short subdomain", func(i *Input) { i.Subdomain = "ab" }, "subdomain"},
		{"uppercase ok but symbols not", func(i *Input) { i.Subdomain = "ac_me" }, "subdomain"},
		{"leading hyphen", func(i *Input) { i.Subdomain = "-acme" }, "subdomain"},
		{"trailing hyphen", func(i *Input) { i.Subdomain = "acme-" }, "subdomain"},
		{"reserved", func(i *Input) { i.Subdomain = "admin" }, "reserved"},
		{"bad currency", func(i *Input) { i.Currency = "KESH" }, "currency"},
	}
	for _, c := range cases {
		in := valid()
		c.mut(&in)
		err := Validate(&in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
		if _, ok := err.(ValidationError); !ok {
			t.Errorf("%s: want ValidationError, got %T", c.name, err)
		}
	}
}

func TestValidateAcceptsHyphenatedSubdomain(t *testing.T) {
	in := valid()
	in.Subdomain = "acme-east-2"
	if err := Validate(&in); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
