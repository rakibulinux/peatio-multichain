package concerns

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fbsobreira/gotron-sdk/pkg/address"
)

type Key struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
}

func NewKey() (*Key, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	publicKey := privateKey.Public()
	if err != nil {
		return nil, err
	}

	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("error format publicKey to ECDSA")
	}

	return &Key{
		privateKey: privateKey,
		publicKey:  publicKeyECDSA,
	}, nil
}

func NewFromPrivateKey(privKey string) (*Key, error) {
	privateKey, err := crypto.HexToECDSA(privKey)
	if err != nil {

		return nil, err
	}

	publicKey := privateKey.Public()
	if err != nil {
		return nil, err
	}

	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("error format publicKey to ECDSA")
	}

	return &Key{
		privateKey: privateKey,
		publicKey:  publicKeyECDSA,
	}, nil
}

func (k *Key) Hex() string {
	return hexutil.Encode(crypto.FromECDSA(k.privateKey))
}

func (k *Key) Address() address.Address {
	return address.PubkeyToAddress(*k.publicKey)
}

func (k *Key) Sign(rawData []byte) (txid string, signature []byte, err error) {
	h256h := sha256.New()
	h256h.Write(rawData)
	hash := h256h.Sum(nil)
	signature, err = crypto.Sign(hash, k.privateKey)
	if err != nil {
		return "", nil, fmt.Errorf("sign error: %v", err)
	}

	return fmt.Sprintf("%x\n", hash), signature, nil
}
