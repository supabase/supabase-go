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

	// EdDSA is JOSE's polymorphic name for EdDSA signing, whose curve a key's
	// crv names.
	// The IANA registry deprecates that name in favor of the fully-specified
	// Ed25519 and Ed448 values (RFC 9864), but EdDSA remains the alg the
	// Supabase Auth server publishes for its Ed25519 signing keys.
	// The hosted platform advertises Ed25519 keys as coming soon (as at
	// September 2026); self-hosted projects can configure them today.
	EdDSA Algorithm = "EdDSA"

	// Ed25519 is RFC 9864's fully specified name for EdDSA over the Ed25519
	// curve, which a token header or a key's alg may carry in place of EdDSA.
	Ed25519 Algorithm = "Ed25519"

	// Ed448 is RFC 9864's fully specified name for EdDSA over the Ed448 curve,
	// which a token header or a key's alg may carry in place of EdDSA. Go's
	// standard library implements no Ed448, so a key on that curve binds no
	// algorithm here.
	Ed448 Algorithm = "Ed448"
)

// Supported reports whether a token declaring alg is checked against the
// project's published keys: ES256, RS256 and EdDSA under any of its names. A
// token declaring any other alg - the legacy HS* family, none or an absent
// alg - is the caller's cue to route to server verification instead. Even
// then, a key binding no algorithm implemented here fails [Key.Verify] with
// [ErrUnusable].
func Supported(alg string) bool {
	switch Algorithm(alg) {
	case ES256, RS256, EdDSA, Ed25519, Ed448:
		return true
	default:
		return false
	}
}

// ErrUnusable is reported by [Key.Verify] for a key this package cannot verify
// with: one binding no algorithm implemented here, or whose material does not
// import.
var ErrUnusable = errors.New("key cannot verify locally")

// errDoesNotVerify is the failure [Key.Verify] reports for a key it can use,
// whatever the cause, so a caller learns nothing beyond "not verified by this
// key".
var errDoesNotVerify = errors.New("signature does not verify with this key")

// Key is one verification key from a project's JWK Set. Its algorithm is
// bound from the key's own kty, crv and alg, never from what a token header
// claims.
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

// bind returns the algorithm the key verifies under, from its kty and crv and
// from its alg when present, which must agree with them, or the empty
// Algorithm when it binds none implemented here. A key on the Ed25519 curve
// binds Ed25519 under any of EdDSA's names.
func (k Key) bind() Algorithm {
	switch {
	case k.keyType == "RSA" && (k.alg == "" || k.alg == RS256):
		return RS256
	case k.keyType == "EC" && k.curve == "P-256" && (k.alg == "" || k.alg == ES256):
		return ES256
	case k.keyType == "OKP" && k.curve == "Ed25519" && (k.alg == "" || k.alg == EdDSA || k.alg == Ed25519):
		return Ed25519
	default:
		return ""
	}
}

// headerNames reports whether a token header's alg names the bound algorithm:
// by that name, or as EdDSA for a key bound to Ed25519.
func headerNames(headerAlgorithm string, bound Algorithm) bool {
	declared := Algorithm(headerAlgorithm)
	return declared == bound || (declared == EdDSA && bound == Ed25519)
}

// Verify checks signature over signingInput using the algorithm bound from
// the key, which headerAlgorithm, the token header's alg, must name, so a
// token cannot dictate how it is verified. A key binding no algorithm
// implemented here, or whose material does not import, fails with
// [ErrUnusable]. Any other error means the signature does not verify with
// this key, a header naming another algorithm included. The caller owns the
// consumer-facing sentinel.
func (k Key) Verify(headerAlgorithm, signingInput string, signature []byte) error {
	bound := k.bind()
	if bound == "" {
		return ErrUnusable
	}
	if !headerNames(headerAlgorithm, bound) {
		return errDoesNotVerify
	}
	digest := sha256.Sum256([]byte(signingInput))

	switch bound {
	case ES256:
		publicKey, err := k.ecdsaPublicKey()
		if err != nil {
			return ErrUnusable
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
			return ErrUnusable
		}
		if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) != nil {
			return errDoesNotVerify
		}
		return nil

	case Ed25519:
		publicKey, err := k.ed25519PublicKey()
		if err != nil {
			return ErrUnusable
		}
		// Ed25519 signs the message itself, not a digest of it.
		if !ed25519.Verify(publicKey, []byte(signingInput), signature) {
			return errDoesNotVerify
		}
		return nil

	default:
		return ErrUnusable
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
		return nil, ErrUnusable
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
		return nil, ErrUnusable
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
