package tron

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/fbsobreira/gotron-sdk/pkg/address"
	"github.com/fbsobreira/gotron-sdk/pkg/client"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/api"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/mergo"
	"github.com/zsmartex/multichain/chains/tron/concerns"
	"github.com/zsmartex/multichain/pkg/currency"
	"github.com/zsmartex/multichain/pkg/transaction"
	"github.com/zsmartex/multichain/pkg/wallet"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const (
	Trc20TransferMethodSignature = "0xa9059cbb"
	Trc20ApproveMethodSignature  = "0x095ea7b3"
	Trc20TransferEventSignature  = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	Trc20NameSignature           = "0x06fdde03"
	Trc20SymbolSignature         = "0x95d89b41"
	Trc20DecimalsSignature       = "0x313ce567"
	Trc20BalanceOf               = "0x70a08231"
)

type Options struct {
	Trc20ContractAddress string          `json:"trc20_contract_address"`
	FeeLimit             decimal.Decimal `json:"fee_limit"` // in SUN
	SubtractFee          bool            `json:"subtract_fee"`
}

var defaultTrxFee = map[string]interface{}{
	"fee_limit": 1_000_000,
}

var defaultTrc20Fee = map[string]interface{}{
	"fee_limit": 10_000_000,
}

type Wallet struct {
	client       *client.GrpcClient
	walletClient api.WalletClient
	currency     *currency.Currency    // selected currency for this wallet
	wallet       *wallet.SettingWallet // selected wallet for this currency
}

func NewWallet() wallet.Wallet {
	return &Wallet{}
}

func (w *Wallet) Configure(settings *wallet.Setting) {
	if settings == nil {
		return
	}

	if settings.Wallet != nil {
		if len(settings.Wallet.URI) > 0 {
			w.client = client.NewGrpcClientWithTimeout(settings.Wallet.URI, 5*time.Second)
			w.client.Start(grpc.WithInsecure())
			w.walletClient = w.client.Client
		}
	}

	if settings.Currency != nil {
		w.currency = settings.Currency
	}

	if settings.Wallet != nil {
		w.wallet = settings.Wallet
	}
}

func (w *Wallet) CreateAddress(ctx context.Context) (address, secret string, err error) {
	key, err := concerns.NewKey()
	if err != nil {
		return "", "", err
	}

	return key.Address().String(), key.Hex(), err
}

func (w *Wallet) PrepareDepositCollection(ctx context.Context, tx *transaction.Transaction, depositSpreads []*transaction.Transaction, depositCurrency *currency.Currency) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultTrc20Fee, depositCurrency.Options)
	if len(options.Trc20ContractAddress) == 0 {
		return nil, nil
	}

	fees := w.ConvertFromBaseUnit(options.FeeLimit)
	amount := fees.Mul(decimal.NewFromInt(int64(len(depositSpreads))))

	tx.Amount = amount

	return w.createTrxTransaction(ctx, tx, nil)
}

func (w *Wallet) CreateTransaction(ctx context.Context, tx *transaction.Transaction, options map[string]interface{}) (*transaction.Transaction, error) {
	if w.currency.Options["trc20_contract_address"] != nil {
		return w.createTrc20Transaction(ctx, tx, options)
	} else {
		return w.createTrxTransaction(ctx, tx, options)
	}
}

func (w *Wallet) createTrxTransaction(ctx context.Context, tx *transaction.Transaction, opt map[string]interface{}) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultTrxFee, w.currency.Options, tx.Options, opt)

	amount := w.ConvertToBaseUnit(tx.Amount)
	fee := options.FeeLimit

	if options.SubtractFee {
		amount = amount.Sub(fee)
	}

	ownerAddress, err := address.Base58ToAddress(w.wallet.Address)
	if err != nil {
		return nil, err
	}

	toAddress, err := address.Base58ToAddress(tx.ToAddress)
	if err != nil {
		return nil, err
	}

	transactionData, err := w.walletClient.CreateTransaction(ctx, &core.TransferContract{
		ToAddress:    toAddress.Bytes(),
		OwnerAddress: ownerAddress.Bytes(),
		Amount:       amount.BigInt().Int64(),
	})
	if err != nil {
		return nil, err
	}

	signedTxn, err := w.signTransaction(ctx, transactionData, w.wallet.Secret)
	if err != nil {
		return nil, err
	}

	txid, err := concerns.TransactionToHex(signedTxn)
	if err != nil {
		return nil, err
	}

	resp, err := w.walletClient.BroadcastTransaction(ctx, signedTxn)
	if err != nil {
		return nil, err
	}

	if !resp.Result {
		return nil, fmt.Errorf("failed to create trx transaction from %s to %s", w.wallet.Address, tx.ToAddress)
	}

	tx.Currency = w.currency.ID
	tx.CurrencyFee = w.currency.ID
	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(fee))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(txid)

	return tx, nil
}

func (w *Wallet) createTrc20Transaction(ctx context.Context, tx *transaction.Transaction, opt map[string]interface{}) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultTrc20Fee, w.currency.Options, tx.Options, opt)

	amount := w.ConvertToBaseUnit(tx.Amount)

	resp, err := w.client.TRC20Send(w.wallet.Address, tx.ToAddress, options.Trc20ContractAddress, amount.BigInt(), options.FeeLimit.BigInt().Int64())
	if err != nil {
		return nil, err
	}

	signedTxn, err := w.signTransaction(ctx, resp.Transaction, w.wallet.Secret)
	if err != nil {
		return nil, err
	}

	txid, err := concerns.TransactionToHex(signedTxn)
	if err != nil {
		return nil, err
	}

	respBroadcast, err := w.walletClient.BroadcastTransaction(ctx, signedTxn)
	if err != nil {
		return nil, err
	}

	if !respBroadcast.Result {
		return nil, fmt.Errorf("failed to create trc20 transaction from %s to %s", w.wallet.Address, tx.ToAddress)
	}

	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(options.FeeLimit))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(txid)

	return tx, nil
}

func (w *Wallet) signTransaction(ctx context.Context, txData *core.Transaction, privateKey string) (transaction *core.Transaction, err error) {
	key, err := concerns.NewFromPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	rawData, err := proto.Marshal(txData.GetRawData())
	if err != nil {
		return nil, fmt.Errorf("proto marshal tx raw data error: %v", err)
	}

	signature, err := key.Sign(rawData)
	if err != nil {
		return nil, err
	}

	txData.Signature = append(txData.Signature, signature)

	return txData, nil
}

func (w *Wallet) LoadBalance(ctx context.Context) (decimal.Decimal, error) {
	if w.currency.Options["trc20_contract_address"] != nil {
		return w.loadTrc20Balance(ctx)
	} else {
		return w.loadTrxBalance(ctx)
	}
}

func (w *Wallet) loadTrxBalance(ctx context.Context) (decimal.Decimal, error) {
	result, err := w.client.GetAccount(w.wallet.Address)
	if err != nil {
		return decimal.Zero, nil
	}

	amount := decimal.NewFromInt(result.Balance)

	return w.ConvertFromBaseUnit(amount), nil
}

func (w *Wallet) loadTrc20Balance(ctx context.Context) (decimal.Decimal, error) {
	big, err := w.client.TRC20ContractBalance(w.wallet.Address, w.currency.Options["trc20_contract_address"].(string))
	if err != nil {
		return decimal.Zero, nil
	}

	return decimal.NewFromBigInt(big, -w.currency.Subunits), nil
}

func (w *Wallet) mergeOptions(first map[string]interface{}, steps ...map[string]interface{}) Options {
	if first == nil {
		first = make(map[string]interface{})
	}

	opts := first

	for _, step := range steps {
		mergo.Merge(&opts, step, mergo.WithOverride)
	}

	bytes, _ := json.Marshal(opts)

	var options Options
	if err := json.Unmarshal(bytes, &options); err != nil {
		return options
	}

	return options
}

func (w *Wallet) ConvertToBaseUnit(amount decimal.Decimal) decimal.Decimal {
	return amount.Mul(decimal.NewFromInt(int64(math.Pow10(int(w.currency.Subunits)))))
}

func (w *Wallet) ConvertFromBaseUnit(amount decimal.Decimal) decimal.Decimal {
	return amount.Div(decimal.NewFromInt(int64(math.Pow10(int(w.currency.Subunits)))))
}
