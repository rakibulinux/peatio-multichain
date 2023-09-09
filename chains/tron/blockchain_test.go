package tron

import (
	"context"
	"testing"

	"github.com/zsmartex/multichain/pkg/blockchain"
	"github.com/zsmartex/multichain/pkg/currency"
)

func newBlockchain() blockchain.Blockchain {
	bl := NewBlockchain()
	bl.Configure(&blockchain.Setting{
		URI: "grpc.nile.trongrid.io:50051",
		Currencies: []*currency.Currency{
			{
				ID:       "TRX",
				Subunits: 6,
			},
			{
				ID:       "USDT",
				Subunits: 6,
				Options: map[string]interface{}{
					"trc20_contract_address": "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf",
				},
			},
		},
	})

	return bl
}

func TestBlockchain_GetLatestBlockNumber(t *testing.T) {
	bl := newBlockchain()

	blockNumber, err := bl.GetLatestBlockNumber(context.Background())
	if err != nil {
		t.Error(err)
	}

	t.Log(blockNumber)
}

func TestBlockchain_GetBlockByNumber(t *testing.T) {
	bl := newBlockchain()

	block, err := bl.GetBlockByNumber(context.Background(), 39870460)
	if err != nil {
		t.Error(err)
	}

	t.Log(block)
}

func TestBlockchain_GetBlockByHash(t *testing.T) {
	bl := newBlockchain()

	block, err := bl.GetBlockByHash(context.Background(), "0000000001876cb35a0f2774d2471bfe497d6c08b2857d663d2118262e585814")
	if err != nil {
		t.Error(err)
	}

	t.Log(block)
}

func TestBlockchain_GetTrxTransaction(t *testing.T) {
	bl := newBlockchain()

	tx, err := bl.GetTransaction(context.Background(), "e602558b952347dd9c9ec6f6e27a45feef97733a37820b6de074714d15e225e9")
	if err != nil {
		t.Error(err)
	}

	t.Error(tx)
}

func TestBlockchain_GetTrc20Transaction(t *testing.T) {
	bl := newBlockchain()

	tx, err := bl.GetTransaction(context.Background(), "a9d2d659a14c402087b208fa3f7063206b441f186f86b14dd2d1d9d90313113e")
	if err != nil {
		t.Error(err)
	}

	t.Error(tx[0])
}

func TestBlockchain_GetBalanceOfAddress(t *testing.T) {
	bl := newBlockchain()

	trxBalance, err := bl.GetBalanceOfAddress(context.Background(), "TNFUgrTZ8ks12qNrZqMMBbAq3h7Y7S4DEq", "TRX")
	if err != nil {
		t.Fatal(err)
	}

	trc20Balance, err := bl.GetBalanceOfAddress(context.Background(), "TV7j9ZYUbMAiVjBq1D7WE3nhTFRAnRSYcr", "USDT")
	if err != nil {
		t.Fatal(err)
	}

	t.Log(trxBalance)
	t.Log(trc20Balance)
}
