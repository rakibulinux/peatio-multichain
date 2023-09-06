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
			URI:     "grpc.nile.trongrid.io:50051",
			Address: "TEy2ekxCANWh6fYUgdhmPDywW3r55ASiRy",
			Secret:  "f2e0dc09d0bdad040e983887432203ef7f20cb105376548bb15c2ad32392d2d6",
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

	t.Error(balance)
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
		Amount:    decimal.NewFromFloat(10),
	}, map[string]interface{}{
		"subtract_fee": true,
	})
	if err != nil {
		t.Error(err)
	}

	t.Log(tx)
	t.Fail()
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
	t.Fail()
}
