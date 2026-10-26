package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndpoint_MarshalJSON_NeverCarriesTheSecret(t *testing.T) {
	cases := map[string]string{
		"plaintext": `{"forward_url":"https://x.test","signature":{"secret":"whsec_abc","enforce":true}}`,
		"encrypted": `{"signature":{"secret_enc":"ZW5jcnlwdGVk","enforce":false}}`,
		"both":      `{"signature":{"secret":"whsec_abc","secret_enc":"ZW5jcnlwdGVk"}}`,
		"nested":    `{"custom_response":{"enabled":true},"signature":{"secret":"whsec_abc"}}`,
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			ep := Endpoint{ID: "e1", Slug: "s", Config: json.RawMessage(cfg)}
			for _, v := range []any{ep, &ep, []Endpoint{ep}, map[string]any{"endpoint": ep}} {
				out, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				s := string(out)
				if strings.Contains(s, "whsec_") || strings.Contains(s, "ZW5jcnlwdGVk") || strings.Contains(s, `"secret"`) || strings.Contains(s, "secret_enc") {
					t.Fatalf("encoded %T leaks the secret: %s", v, s)
				}
				if !strings.Contains(s, `"secret_set":true`) {
					t.Errorf("encoded %T lacks secret_set: %s", v, s)
				}
			}
			if !strings.Contains(string(ep.Config), "secret") {
				t.Error("marshalling changed the stored config")
			}
		})
	}
}

func TestRedactSecrets_LeavesOtherConfigsAlone(t *testing.T) {
	for _, cfg := range []string{``, `{}`, `{"forward_url":"https://x.test"}`, `{"signature":{"enforce":true}}`, `{"signature":"odd"}`, `not json`} {
		got := RedactSecrets(json.RawMessage(cfg))
		if strings.Contains(string(got), "secret_set") {
			t.Errorf("RedactSecrets(%q) added secret_set: %s", cfg, got)
		}
		if cfg == `{"signature":"odd"}` || cfg == `not json` || cfg == `` {
			if string(got) != cfg {
				t.Errorf("RedactSecrets(%q) = %q, want unchanged", cfg, got)
			}
		}
	}
}
