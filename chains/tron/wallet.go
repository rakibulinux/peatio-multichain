package tron

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/go-resty/resty/v2"
	"github.com/huandu/xstrings"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/mergo"
	"github.com/zsmartex/multichain/chains/tron/concerns"
	"github.com/zsmartex/multichain/pkg/currency"
	"github.com/zsmartex/multichain/pkg/transaction"
	"github.com/zsmartex/multichain/pkg/wallet"
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
	client   *resty.Client
	currency *currency.Currency    // selected currency for this wallet
	wallet   *wallet.SettingWallet // selected wallet for this currency
}

func NewWallet() wallet.Wallet {
	return &Wallet{
		client: resty.New(),
	}
}

func (w *Wallet) Configure(settings *wallet.Setting) {
	if settings.Currency != nil {
		w.currency = settings.Currency
	}

	if settings.Wallet != nil {
		w.wallet = settings.Wallet
	}
}

func (w *Wallet) jsonRPC(ctx context.Context, resp interface{}, method string, params interface{}) error {
	type Result struct {
		Code    string           `json:"code,omitempty"`
		Message string           `json:"message,omitempty"`
		Error   *json.RawMessage `json:"Error,omitempty"`
	}

	uri, err := url.Parse(w.wallet.URI)
	if err != nil {
		return err
	}

	response, err := w.client.
		R().
		SetContext(ctx).
		SetResult(Result{}).
		SetHeaders(map[string]string{
			"Accept":       "application/json",
			"Content-Type": "application/json",
		}).
		SetBody(params).
		Post(uri.JoinPath(method).String())

	if err != nil {
		return err
	}

	result := response.Result().(*Result)

	if result.Code == "CONTRACT_VALIDATE_ERROR" {
		decoded, err := hex.DecodeString(result.Message)
		if err != nil {
			return fmt.Errorf("jsonRPC error: %s, %s", result.Code, err.Error())
		}
		return fmt.Errorf("jsonRPC error: %s, %s", result.Code, string(decoded))
	}

	if result.Error != nil {
		return errors.New("jsonRPC error: " + string(*result.Error))
	}

	if err := json.Unmarshal(response.Body(), resp); err != nil {
		return err
	}

	fmt.Println(string(response.Body()))

	return nil
}

func (w *Wallet) CreateAddress(ctx context.Context) (address, secret string, err error) {
	type Result struct {
		Address    string `json:"address"`
		PrivateKey string `json:"privateKey"`
	}
	var resp *Result
	err = w.jsonRPC(ctx, &resp, "wallet/generateaddress", nil)
	if err != nil {
		return
	}

	return resp.Address, resp.PrivateKey, err
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

	toAddress, err := concerns.Base58ToAddress(tx.ToAddress)
	if err != nil {
		return nil, err
	}

	amount := w.ConvertToBaseUnit(tx.Amount)
	fee := options.FeeLimit

	if options.SubtractFee {
		amount = amount.Sub(fee)
	}

	var resp *struct {
		Transaction struct {
			TxID string `json:"txID"`
		} `json:"transaction"`
	}

	if err := w.jsonRPC(ctx, &resp, "wallet/easytransferbyprivate", map[string]interface{}{
		"privateKey": w.wallet.Secret,
		"toAddress":  toAddress.Hex(),
		"amount":     amount.BigInt(),
	}); err != nil {
		return nil, err
	}

	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(fee))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(resp.Transaction.TxID)

	return tx, nil
}

func (w *Wallet) createTrc20Transaction(ctx context.Context, tx *transaction.Transaction, opt map[string]interface{}) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultTrc20Fee, w.currency.Options, tx.Options, opt)

	signedTxn, err := w.signTransaction(ctx, tx, options)
	if err != nil {
		return nil, err
	}

	resp := new(struct {
		Result bool `json:"result"`
	})
	if err := w.jsonRPC(ctx, &resp, "wallet/broadcasttransaction", signedTxn); err != nil || !resp.Result {
		return nil, fmt.Errorf("failed to create trc20 transaction from %s to %s", w.wallet.Address, tx.ToAddress)
	}

	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(options.FeeLimit))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(signedTxn["txID"].(string))

	return tx, nil
}

func (w *Wallet) signTransaction(ctx context.Context, tx *transaction.Transaction, options Options) (map[string]interface{}, error) {
	txn, err := w.triggerSmartContract(ctx, tx, options)
	if err != nil {
		return nil, err
	}

	var resp map[string]interface{}
	if err := w.jsonRPC(ctx, &resp, "wallet/gettransactionsign", map[string]interface{}{
		"transaction": txn,
		"privateKey":  w.wallet.Secret,
	}); err != nil {
		return nil, err
	}

	return resp, nil
}

func (w *Wallet) triggerSmartContract(ctx context.Context, tx *transaction.Transaction, options Options) (json.RawMessage, error) {
	contractAddress, err := concerns.Base58ToAddress(options.Trc20ContractAddress)
	if err != nil {
		return nil, err
	}

	ownerAddress, err := concerns.Base58ToAddress(w.wallet.Address)
	if err != nil {
		return nil, err
	}

	toAddress, err := concerns.Base58ToAddress(tx.ToAddress)
	if err != nil {
		return nil, err
	}

	type respResult struct {
		Transaction json.RawMessage `json:"transaction"`
	}

	amount := w.ConvertToBaseUnit(tx.Amount)
	hexAmount := hexutil.EncodeBig(amount.BigInt())
	parameter := xstrings.RightJustify(toAddress.Hex()[2:], 64, "0") + xstrings.RightJustify(strings.TrimLeft(hexAmount, "0x"), 64, "0")

	var result *respResult
	if err := w.jsonRPC(ctx, &result, "wallet/triggersmartcontract", map[string]interface{}{
		"contract_address":  contractAddress.Hex(),
		"function_selector": "transfer(address,uint256)",
		"parameter":         parameter,
		"fee_limit":         options.FeeLimit,
		"owner_address":     ownerAddress.Hex(),
	}); err != nil {
		return nil, err
	}

	return result.Transaction, nil
}

func (w *Wallet) LoadBalance(ctx context.Context) (decimal.Decimal, error) {
	if w.currency.Options["trc20_contract_address"] != nil {
		return w.loadTrc20Balance(ctx)
	} else {
		return w.loadTrxBalance(ctx)
	}
}

func (w *Wallet) loadTrc20Balance(ctx context.Context) (decimal.Decimal, error) {
	contractAddress, err := concerns.Base58ToAddress(w.currency.Options["trc20_contract_address"].(string))
	if err != nil {
		return decimal.Zero, err
	}

	ownerAddress, err := concerns.Base58ToAddress(w.wallet.Address)
	if err != nil {
		return decimal.Zero, err
	}

	var resp *struct {
		ConstantResult []string `json:"constant_result"`
	}

	if err := w.jsonRPC(ctx, &resp, "wallet/triggersmartcontract", map[string]string{
		"owner_address":     ownerAddress.Hex(),
		"contract_address":  contractAddress.Hex(),
		"function_selector": "balanceOf(address)",
		"parameter":         xstrings.RightJustify(ownerAddress.Hex()[2:], 64, "0"),
	}); err != nil {
		return decimal.Zero, err
	}

	b := &big.Int{}
	b.SetString(resp.ConstantResult[0], 16)

	return decimal.NewFromBigInt(b, -w.currency.Subunits), nil
}

func (w *Wallet) loadTrxBalance(ctx context.Context) (decimal.Decimal, error) {
	addressDecoded, err := concerns.Base58ToAddress(w.wallet.Address)
	if err != nil {
		return decimal.Zero, err
	}

	type Result struct {
		Balance decimal.Decimal `json:"balance"`
	}

	var result *Result
	if err := w.jsonRPC(ctx, &result, "wallet/getaccount", map[string]interface{}{
		"address": addressDecoded.Hex(),
	}); err != nil {
		return decimal.Zero, err
	}

	return w.ConvertFromBaseUnit(result.Balance), nil
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
