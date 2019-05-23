package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/kelseyhightower/envconfig"
	crypto "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"
)

const configKey = "cluster"

// Identity defaults
const (
	DefaultConfigCrypto    = crypto.Ed25519
	DefaultConfigKeyLength = -1
)

// Identity represents identity of a cluster peer for communication,
// including the Consensus component.
type Identity struct {
	ID         peer.ID
	PrivateKey crypto.PrivKey
}

// identityJSON represents a Cluster peer identity as it will look when it is
// saved using JSON.
type identityJSON struct {
	ID         string `json:"id"`
	PrivateKey string `json:"private_key"`
}

// NewIdentity generate a public-private keypair and returns a new Identity.
func NewIdentity() (*Identity, error) {
	// pid and private key generation
	priv, pub, err := crypto.GenerateKeyPair(
		DefaultConfigCrypto,
		DefaultConfigKeyLength,
	)
	if err != nil {
		return nil, err
	}
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, err
	}

	return &Identity{
		ID:         pid,
		PrivateKey: priv,
	}, nil
}

// ConfigKey returns a human-readable string to identify
// a cluster Identity.
func (ident *Identity) ConfigKey() string {
	return configKey
}

// SaveJSON saves the JSON representation of the Identity to
// the given path.
func (ident *Identity) SaveJSON(path string) error {
	logger.Info("Saving identity")

	bs, err := ident.ToJSON()
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, bs, 0600)
}

// ToJSON generates a human-friendly version of Identity.
func (ident *Identity) ToJSON() (raw []byte, err error) {
	jID, err := ident.toIdentityJSON()
	if err != nil {
		return
	}

	raw, err = json.MarshalIndent(jID, "", "    ")
	return
}

func (ident *Identity) toIdentityJSON() (jID *identityJSON, err error) {
	jID = &identityJSON{}

	// Private Key
	pkeyBytes, err := ident.PrivateKey.Bytes()
	if err != nil {
		return
	}
	pKey := base64.StdEncoding.EncodeToString(pkeyBytes)

	// Set all identity fields
	jID.ID = ident.ID.Pretty()
	jID.PrivateKey = pKey
	return
}

// LoadJSON receives a raw json-formatted identity and
// sets the Config fields from it. Note that it should be JSON
// as generated by ToJSON().
func (ident *Identity) LoadJSON(raw []byte) error {
	jID := &identityJSON{}
	err := json.Unmarshal(raw, jID)
	if err != nil {
		logger.Error("Error unmarshaling cluster config")
		return err
	}

	return ident.applyIdentityJSON(jID)
}

func (ident *Identity) applyIdentityJSON(jID *identityJSON) error {
	pid, err := peer.IDB58Decode(jID.ID)
	if err != nil {
		err = fmt.Errorf("error decoding cluster ID: %s", err)
		return err
	}
	ident.ID = pid

	pkb, err := base64.StdEncoding.DecodeString(jID.PrivateKey)
	if err != nil {
		err = fmt.Errorf("error decoding private_key: %s", err)
		return err
	}
	pKey, err := crypto.UnmarshalPrivateKey(pkb)
	if err != nil {
		err = fmt.Errorf("error parsing private_key ID: %s", err)
		return err
	}
	ident.PrivateKey = pKey

	return ident.Validate()
}

// Validate will check that the values of this identity
// seem to be working ones.
func (ident *Identity) Validate() error {
	if ident.ID == "" {
		return errors.New("identity ID not set")
	}

	if ident.PrivateKey == nil {
		return errors.New("no identity private_key set")
	}

	if !ident.ID.MatchesPrivateKey(ident.PrivateKey) {
		return errors.New("identity ID does not match the private_key")
	}
	return nil
}

// LoadJSONFromFile reads an Identity file from disk and parses
// it and return Identity.
func (ident *Identity) LoadJSONFromFile(path string) error {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		logger.Error("error reading the configuration file: ", err)
		return err
	}

	return ident.LoadJSON(file)
}

// ApplyEnvVars fills in any Config fields found
// as environment variables.
func (ident *Identity) ApplyEnvVars() error {
	jID, err := ident.toIdentityJSON()
	if err != nil {
		return err
	}
	err = envconfig.Process(ident.ConfigKey(), jID)
	if err != nil {
		return err
	}
	return ident.applyIdentityJSON(jID)
}

// Equals returns true if equal to provided identity.
func (ident *Identity) Equals(i *Identity) bool {
	return ident.ID == i.ID && ident.PrivateKey.Equals(i.PrivateKey)
}
