// Package key models one verification key from a project's JWK Set: the
// algorithm vocabulary, the JWK wire shape and signature verification bound
// to key material rather than to what a token header claims. Nothing is
// retained and nothing is fetched here - a JWK Set document comes in through
// [ParseSet] and the resulting keys verify signatures, nothing more.
package key

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
)

// Algorithm is a JWT alg header value, as registered in the IANA JOSE
// registry (https://www.iana.org/assignments/jose).
// The Supabase docs describe the algorithms available for project signing keys:
// https://supabase.com/docs/guides/auth/signing-keys#choosing-the-right-signing-algorithm
type Algorithm string

const (
	// ES256 is ECDSA using the NIST P-256 curve and SHA-256. The recommended
	// algorithm for use with Supabase.
	ES256 Algorithm = "ES256"

	// RS256 is RSASSA-PKCS1-v1_5 using SHA-256; Supabase issues 2048-bit keys
	// for it (the docs' "RSA 2048" row).
	RS256 Algorithm = "RS256"

	// EdDSA is Ed25519 signing under JOSE's polymorphic EdDSA name.
	// The IANA registry deprecates that name in favor of the fully-specified
	// Ed25519 and Ed448 values (RFC 9864), but EdDSA remains the alg the
	// Supabase Auth server publishes for its Ed25519 signing keys.
	// The hosted platform advertises Ed25519 keys as coming soon (as at
	// September 2026); self-hosted projects can configure them today.
	EdDSA Algorithm = "EdDSA"
)

// Supported reports whether alg is one this package verifies locally against a
// published signing key. A token declaring any other alg - the legacy HS*
// family, none, or an absent alg - is the caller's cue to route to server
// verification instead.
func Supported(alg string) bool {
	switch Algorithm(alg) {
	case ES256, RS256, EdDSA:
		return true
	default:
		return false
	}
}

// errDoesNotVerify is the single failure [Key.Verify] reports, whatever the
// cause, so a caller learns nothing beyond "not verified by this key".
var errDoesNotVerify = errors.New("signature does not verify with this key")

// Key is one verification key from a project's JWK Set. Its algorithm is
// bound to the key material - the key's own alg field, or one inferred from
// its type - never to what a token header claims.
type Key struct {
	keyType  string
	keyID    string
	alg      Algorithm
	curve    string
	x        string
	y        string
	modulus  string
	exponent string
}

// ID returns the key id the JWK Set published for this key.
func (k Key) ID() string { return k.keyID }

// algorithm returns the JWT alg the key verifies. It prefers the key's own alg
// and infers one from the key type when the field is absent.
func (k Key) algorithm() Algorithm {
	if k.alg != "" {
		return k.alg
	}
	switch {
	case k.keyType == "RSA":
		return RS256
	case k.keyType == "EC" && k.curve == "P-256":
		return ES256
	case k.keyType == "OKP" && k.curve == "Ed25519":
		return EdDSA
	default:
		return ""
	}
}

// Verify checks signature over signingInput using the algorithm the key
// declares rather than the one the token header claims, so a token cannot
// dictate how it is verified. Any error means the signature does not verify
// with this key; the caller owns the consumer-facing sentinel.
func (k Key) Verify(signingInput string, signature []byte) error {
	digest := sha256.Sum256([]byte(signingInput))

	switch k.algorithm() {
	case ES256:
		publicKey, err := k.ecdsaPublicKey()
		if err != nil {
			return errDoesNotVerify
		}
		// A JWS ECDSA signature is the fixed-width r and s values concatenated,
		// not the ASN.1 form ecdsa.Verify's sibling expects.
		if len(signature) != 64 {
			return errDoesNotVerify
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		if !ecdsa.Verify(publicKey, digest[:], r, s) {
			return errDoesNotVerify
		}
		return nil

	case RS256:
		publicKey, err := k.rsaPublicKey()
		if err != nil {
			return errDoesNotVerify
		}
		if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) != nil {
			return errDoesNotVerify
		}
		return nil

	case EdDSA:
		publicKey, err := k.ed25519PublicKey()
		if err != nil {
			return errDoesNotVerify
		}
		// Ed25519 signs the message itself, not a digest of it.
		if !ed25519.Verify(publicKey, []byte(signingInput), signature) {
			return errDoesNotVerify
		}
		return nil

	default:
		return errDoesNotVerify
	}
}

func (k Key) ecdsaPublicKey() (*ecdsa.PublicKey, error) {
	x, err := base64.RawURLEncoding.DecodeString(k.x)
	if err != nil {
		return nil, err
	}
	y, err := base64.RawURLEncoding.DecodeString(k.y)
	if err != nil {
		return nil, err
	}

	// A P-256 coordinate is exactly 32 bytes, both in a JWK (RFC 7518,
	// section 6.2.1.2) and in the SEC 1 uncompressed point form. Undersize
	// coordinates - a publisher stripping leading zeros - name the same
	// integers, so they are left-padded rather than rejected; oversize ones
	// are rejected.
	const coordinateSize = 32
	if len(x) > coordinateSize || len(y) > coordinateSize {
		return nil, errDoesNotVerify
	}
	point := make([]byte, 1+2*coordinateSize)
	point[0] = 4 // SEC 1 uncompressed point form
	copy(point[1+coordinateSize-len(x):], x)
	copy(point[1+2*coordinateSize-len(y):], y)
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
}

func (k Key) rsaPublicKey() (*rsa.PublicKey, error) {
	modulus, err := base64.RawURLEncoding.DecodeString(k.modulus)
	if err != nil {
		return nil, err
	}
	exponent, err := base64.RawURLEncoding.DecodeString(k.exponent)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(new(big.Int).SetBytes(exponent).Int64()),
	}, nil
}

func (k Key) ed25519PublicKey() (ed25519.PublicKey, error) {
	x, err := base64.RawURLEncoding.DecodeString(k.x)
	if err != nil {
		return nil, err
	}
	if len(x) != ed25519.PublicKeySize {
		return nil, errDoesNotVerify
	}
	return ed25519.PublicKey(x), nil
}

// keyWire is the subset of a JWK's fields this package reads to build a
// verification key. The Auth server publishes only public keys here.
type keyWire struct {
	KeyType   string    `json:"kty"`
	KeyID     string    `json:"kid"`
	Algorithm Algorithm `json:"alg"`
	Curve     string    `json:"crv"`
	X         string    `json:"x"`
	Y         string    `json:"y"`
	Modulus   string    `json:"n"`
	Exponent  string    `json:"e"`
}

// ParseSet decodes a JWK Set document into its keys, returning the decode
// error verbatim when the document is not the documented JSON shape. An empty
// set - a project with no asymmetric signing keys - yields no keys.
func ParseSet(document []byte) ([]Key, error) {
	var wire struct {
		Keys []keyWire `json:"keys"`
	}
	if err := json.Unmarshal(document, &wire); err != nil {
		return nil, err
	}

	keys := make([]Key, 0, len(wire.Keys))
	for _, entry := range wire.Keys {
		keys = append(keys, Key{
			keyType:  entry.KeyType,
			keyID:    entry.KeyID,
			alg:      entry.Algorithm,
			curve:    entry.Curve,
			x:        entry.X,
			y:        entry.Y,
			modulus:  entry.Modulus,
			exponent: entry.Exponent,
		})
	}
	return keys, nil
}
