package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
)

// algorithm is a JWT alg header value, as registered in the IANA JOSE
// registry (https://www.iana.org/assignments/jose).
// The Supabase docs describe the algorithms available for project signing keys:
// https://supabase.com/docs/guides/auth/signing-keys#choosing-the-right-signing-algorithm
type algorithm string

const (
	// algorithmES256 is ECDSA using the NIST P-256 curve and SHA-256. The
	// recommended algorithm for use with Supabase.
	algorithmES256 algorithm = "ES256"

	// algorithmRS256 is RSASSA-PKCS1-v1_5 using SHA-256; Supabase issues
	// 2048-bit keys for it (the docs' "RSA 2048" row).
	algorithmRS256 algorithm = "RS256"

	// algorithmEdDSA is Ed25519 signing under JOSE's polymorphic EdDSA name.
	// The IANA registry deprecates that name in favor of the fully-specified
	// Ed25519 and Ed448 values (RFC 9864), but EdDSA remains the alg the
	// Supabase Auth server publishes for its Ed25519 signing keys.
	// The hosted platform advertises Ed25519 keys as coming soon (as at
	// September 2026); self-hosted projects can configure them today.
	algorithmEdDSA algorithm = "EdDSA"
)

// asymmetricAlgorithms is the set of JWT alg values this package verifies
// locally against a published signing key. A token declaring any other alg -
// the legacy HS* family, none, or an absent alg - routes to server
// verification instead.
var asymmetricAlgorithms = map[algorithm]bool{
	algorithmES256: true,
	algorithmRS256: true,
	algorithmEdDSA: true,
}

// decodedToken holds the three parts of a parsed JWT: the decoded header and
// claims bytes, the signature bytes, and the raw base64url header and payload
// segments whose "header.payload" join is the input a signature covers.
type decodedToken struct {
	header       tokenHeader
	claimsBytes  []byte
	signature    []byte
	signingInput string
}

// tokenHeader is the subset of JWT header fields routing and verification need.
type tokenHeader struct {
	Algorithm algorithm `json:"alg"`
	KeyID     string    `json:"kid"`
}

// decodeToken splits and base64url-decodes a JWT into its parts. It reports
// [ErrMalformedJWT] when the token is not three base64url segments carrying a
// JSON header.
func decodeToken(token string) (decodedToken, error) {
	firstDot := strings.IndexByte(token, '.')
	lastDot := strings.LastIndexByte(token, '.')
	if firstDot <= 0 || lastDot <= firstDot || lastDot == len(token)-1 {
		return decodedToken{}, ErrMalformedJWT
	}
	rawHeader := token[:firstDot]
	rawPayload := token[firstDot+1 : lastDot]
	rawSignature := token[lastDot+1:]
	if strings.IndexByte(rawPayload, '.') != -1 {
		return decodedToken{}, ErrMalformedJWT
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(rawHeader)
	if err != nil {
		return decodedToken{}, ErrMalformedJWT
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(rawPayload)
	if err != nil {
		return decodedToken{}, ErrMalformedJWT
	}
	signature, err := base64.RawURLEncoding.DecodeString(rawSignature)
	if err != nil {
		return decodedToken{}, ErrMalformedJWT
	}

	var header tokenHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return decodedToken{}, ErrMalformedJWT
	}

	return decodedToken{
		header:       header,
		claimsBytes:  claimsBytes,
		signature:    signature,
		signingInput: token[:lastDot],
	}, nil
}

// verifySignature checks the token's signature against key, using the algorithm
// the key declares rather than the one the token header claims, so a token
// cannot dictate how it is verified. It reports [ErrInvalidSignature] when the
// signature does not verify or the key cannot be used.
func verifySignature(token decodedToken, key jsonWebKey) error {
	digest := sha256.Sum256([]byte(token.signingInput))

	switch key.algorithm() {
	case algorithmES256:
		publicKey, err := key.ecdsaPublicKey()
		if err != nil {
			return ErrInvalidSignature
		}
		// A JWS ECDSA signature is the fixed-width r and s values concatenated,
		// not the ASN.1 form ecdsa.Verify's sibling expects.
		if len(token.signature) != 64 {
			return ErrInvalidSignature
		}
		r := new(big.Int).SetBytes(token.signature[:32])
		s := new(big.Int).SetBytes(token.signature[32:])
		if !ecdsa.Verify(publicKey, digest[:], r, s) {
			return ErrInvalidSignature
		}
		return nil

	case algorithmRS256:
		publicKey, err := key.rsaPublicKey()
		if err != nil {
			return ErrInvalidSignature
		}
		if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], token.signature) != nil {
			return ErrInvalidSignature
		}
		return nil

	case algorithmEdDSA:
		publicKey, err := key.ed25519PublicKey()
		if err != nil {
			return ErrInvalidSignature
		}
		// Ed25519 signs the message itself, not a digest of it.
		if !ed25519.Verify(publicKey, []byte(token.signingInput), token.signature) {
			return ErrInvalidSignature
		}
		return nil

	default:
		return ErrInvalidSignature
	}
}

// jsonWebKey is the subset of a JWK's fields this package reads to build a
// verification key. The Auth server publishes only public keys here.
type jsonWebKey struct {
	KeyType   string    `json:"kty"`
	KeyID     string    `json:"kid"`
	Algorithm algorithm `json:"alg"`
	Curve     string    `json:"crv"`
	X         string    `json:"x"`
	Y         string    `json:"y"`
	Modulus   string    `json:"n"`
	Exponent  string    `json:"e"`
}

// algorithm returns the JWT alg the key verifies. It prefers the key's own alg
// and infers one from the key type when the field is absent.
func (k jsonWebKey) algorithm() algorithm {
	if k.Algorithm != "" {
		return k.Algorithm
	}
	switch {
	case k.KeyType == "RSA":
		return algorithmRS256
	case k.KeyType == "EC" && k.Curve == "P-256":
		return algorithmES256
	case k.KeyType == "OKP" && k.Curve == "Ed25519":
		return algorithmEdDSA
	default:
		return ""
	}
}

func (k jsonWebKey) ecdsaPublicKey() (*ecdsa.PublicKey, error) {
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	y, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}

func (k jsonWebKey) rsaPublicKey() (*rsa.PublicKey, error) {
	modulus, err := base64.RawURLEncoding.DecodeString(k.Modulus)
	if err != nil {
		return nil, err
	}
	exponent, err := base64.RawURLEncoding.DecodeString(k.Exponent)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(new(big.Int).SetBytes(exponent).Int64()),
	}, nil
}

func (k jsonWebKey) ed25519PublicKey() (ed25519.PublicKey, error) {
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	if len(x) != ed25519.PublicKeySize {
		return nil, ErrInvalidSignature
	}
	return ed25519.PublicKey(x), nil
}
