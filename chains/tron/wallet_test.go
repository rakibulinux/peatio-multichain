package tron

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zsmartex/multichain/pkg/currency"
	"github.com/zsmartex/multichain/pkg/transaction"
	"github.com/zsmartex/multichain/pkg/wallet"
)

func newWallet() wallet.Wallet {
	w := NewWallet()

	w.Configure(&wallet.Setting{
		Wallet: &wallet.SettingWallet{
			URI:     "http://demo.zsmartex.com:8090",
			Address: "TVuwqpZ3a8gd2BFG6aiYBs1RaA6KmdHdcr",
			Secret:  "d70cd214c7ee93646b11a8e9db7ad10d8f48b755deec3955398214a4657bae4b",
		},
	})

	return w
}

func TestWallet_LoadTrxBalance(t *testing.T) {
	w := newWallet()

	w.Configure(&wallet.Setting{
		Currency: &currency.Currency{
			ID:       "TRX",
			Subunits: 6,
		},
	})

	balance, err := w.LoadBalance(context.Background())
	if err != nil {
		t.Error(err)
	}

	t.Log(balance)
}

func TestWallet_LoadTrc20Balance(t *testing.T) {
	w := newWallet()

	w.Configure(&wallet.Setting{
		Currency: &currency.Currency{
			ID:       "USDT",
			Subunits: 6,
			Options: map[string]interface{}{
				"trc20_contract_address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
			},
		},
	})

	balance, err := w.LoadBalance(context.Background())
	if err != nil {
		t.Error(err)
	}

	t.Log(balance)
}

func TestWallet_CreateAddress(t *testing.T) {
	w := newWallet()

	w.Configure(&wallet.Setting{
		Currency: &currency.Currency{
			ID:       "TRX",
			Subunits: 6,
		},
	})

	address, secret, err := w.CreateAddress(context.Background())
	if err != nil {
		t.Error(err)
	}

	t.Log(address, secret)
}

func TestWallet_CreateTrxTransaction(t *testing.T) {
	w := newWallet()

	w.Configure(&wallet.Setting{
		Currency: &currency.Currency{
			ID:       "TRX",
			Subunits: 6,
		},
	})

	tx, err := w.CreateTransaction(context.Background(), &transaction.Transaction{
		ToAddress: "TGKFmSijnD6iNLgaf7CbQVysw81MTDbvHq",
		Amount:    decimal.NewFromFloat(30),
	}, nil)
	if err != nil {
		t.Error(err)
	}

	t.Log(tx)
}

func TestWallet_CreateTrc20Transaction(t *testing.T) {
	w := newWallet()

	w.Configure(&wallet.Setting{
		Currency: &currency.Currency{
			ID:       "USDT",
			Subunits: 6,
			Options: map[string]interface{}{
				"trc20_contract_address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
			},
		},
	})

	tx, err := w.CreateTransaction(context.Background(), &transaction.Transaction{
		ToAddress:   "TGKFmSijnD6iNLgaf7CbQVysw81MTDbvHq",
		Amount:      decimal.NewFromFloat(30),
		Currency:    "USDT",
		CurrencyFee: "TRX",
	}, nil)
	if err != nil {
		t.Error(err)
	}

	t.Log(tx)
}
