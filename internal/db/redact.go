package db

import "encoding/json"

// MarshalJSON encodes the endpoint with its signature secret left out, so no
// response, message or log that encodes an Endpoint can carry it. The stored
// config is untouched: read ep.Config directly for the sealed value.
func (ep Endpoint) MarshalJSON() ([]byte, error) {
	type plain Endpoint // no MarshalJSON, so no recursion
	p := plain(ep)
	p.Config = RedactSecrets(ep.Config)
	return json.Marshal(p)
}

// RedactSecrets returns cfg with signature.secret and signature.secret_enc
// removed and signature.secret_set true when either was present. A config
// without a signature object is returned as is.
func RedactSecrets(cfg json.RawMessage) json.RawMessage {
	var top map[string]json.RawMessage
	if len(cfg) == 0 || json.Unmarshal(cfg, &top) != nil || top == nil {
		return cfg
	}
	raw, has := top["signature"]
	if !has {
		return cfg
	}
	var sig map[string]any
	if json.Unmarshal(raw, &sig) != nil || sig == nil {
		return cfg
	}
	if s, _ := sig["secret"].(string); s != "" {
		sig["secret_set"] = true
	}
	if s, _ := sig["secret_enc"].(string); s != "" {
		sig["secret_set"] = true
	}
	delete(sig, "secret")
	delete(sig, "secret_enc")
	sigRaw, err := json.Marshal(sig)
	if err != nil {
		return cfg
	}
	top["signature"] = sigRaw
	out, err := json.Marshal(top)
	if err != nil {
		return cfg
	}
	return out
}
