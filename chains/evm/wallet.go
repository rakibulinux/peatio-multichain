package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/mergo"
	"github.com/zsmartex/multichain/pkg/currency"
	"github.com/zsmartex/multichain/pkg/transaction"
	"github.com/zsmartex/multichain/pkg/wallet"
)

type Options struct {
	Erc20ContractAddress string              `json:"erc20_contract_address"`
	GasPrice             *big.Int            `json:"gas_price"`
	GasLimit             uint64              `json:"gas_limit"`
	GasRate              wallet.GasPriceRate `json:"gas_rate"`
	SubtractFee          bool                `json:"subtract_fee"`
}

var defaultEvmFee = map[string]interface{}{
	"gas_limit": 21_000,
	"gas_rate":  wallet.GasPriceRateStandard,
}

var defaultErc20Fee = map[string]interface{}{
	"gas_limit": 90_000,
	"gas_rate":  wallet.GasPriceRateStandard,
}

type Wallet struct {
	client   *ethclient.Client
	currency *currency.Currency    // selected currency for this wallet
	wallet   *wallet.SettingWallet // selected wallet for this currency
}

func NewWallet() wallet.Wallet {
	return &Wallet{}
}

func (w *Wallet) Configure(settings *wallet.Setting) {
	if settings.Wallet != nil {
		w.wallet = settings.Wallet

		rpcClient, err := rpc.Dial(settings.Wallet.URI)
		if err != nil {
			panic(err)
		}

		w.client = ethclient.NewClient(rpcClient)
	}

	if settings.Currency != nil {
		w.currency = settings.Currency
	}
}

func (w *Wallet) CreateAddress(_ context.Context) (address, secret string, err error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return "", "", err
	}

	privateKeyBytes := crypto.FromECDSA(privateKey)
	secret = hexutil.Encode(privateKeyBytes)

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", "", err
	}

	address = crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

	return address, secret, nil
}

func (w *Wallet) PrepareDepositCollection(ctx context.Context, tx *transaction.Transaction, depositSpreads []*transaction.Transaction, depositCurrency *currency.Currency) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultErc20Fee, depositCurrency.Options)

	if len(options.Erc20ContractAddress) == 0 {
		return nil, nil
	}

	if options.GasPrice.Cmp(big.NewInt(0)) == 0 {
		gasPrice, err := w.calculateGasPrice(ctx, options)
		if err != nil {
			return nil, err
		}

		options.GasPrice = gasPrice
	}

	fees := decimal.NewFromBigInt(new(big.Int).Mul(options.GasPrice, big.NewInt(int64(options.GasLimit))), -w.currency.Subunits)
	amount := fees.Mul(decimal.NewFromInt(int64(len(depositSpreads))))

	tx.Amount = amount

	return w.createEvmTransaction(ctx, tx, nil)
}

func (w *Wallet) CreateTransaction(ctx context.Context, tx *transaction.Transaction, options map[string]interface{}) (*transaction.Transaction, error) {
	if len(w.ContractAddress()) > 0 {
		return w.createErc20Transaction(ctx, tx, options)
	} else {
		return w.createEvmTransaction(ctx, tx, options)
	}
}

func (w *Wallet) createEvmTransaction(ctx context.Context, tx *transaction.Transaction, opt map[string]interface{}) (t *transaction.Transaction, err error) {
	options := w.mergeOptions(defaultEvmFee, w.currency.Options, tx.Options, opt)

	if options.GasPrice.Cmp(big.NewInt(0)) == 0 {
		gasPrice, err := w.calculateGasPrice(ctx, options)
		if err != nil {
			return nil, err
		}

		options.GasPrice = gasPrice
	}

	amount := w.ConvertToBaseUnit(tx.Amount)
	fee := decimal.NewFromBigInt(new(big.Int).Mul(options.GasPrice, big.NewInt(int64(options.GasLimit))), 0)

	if options.SubtractFee {
		amount = amount.Sub(fee)
	}

	fromAddress := common.HexToAddress(w.normalizeAddress(w.wallet.Address))
	toAddress := common.HexToAddress(w.normalizeAddress(tx.ToAddress))

	nonce, err := w.client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return nil, err
	}

	chainID, err := w.client.NetworkID(context.Background())
	if err != nil {
		return nil, err
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(w.wallet.Secret, "0x"))
	if err != nil {
		return nil, err
	}

	signedTx, err := types.SignTx(
		types.NewTx(&types.LegacyTx{
			Gas:      options.GasLimit,
			GasPrice: options.GasPrice,
			Nonce:    nonce,
			Value:    amount.BigInt(),
			To:       &toAddress,
		}),
		types.NewEIP155Signer(chainID),
		privateKey,
	)

	if err := w.client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}

	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(fee))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(signedTx.Hash().Hex())

	return tx, nil
}

func (w *Wallet) createErc20Transaction(ctx context.Context, tx *transaction.Transaction, opt map[string]interface{}) (*transaction.Transaction, error) {
	options := w.mergeOptions(defaultEvmFee, w.currency.Options, tx.Options, opt)

	if options.GasPrice.Cmp(big.NewInt(0)) == 0 {
		gasPrice, err := w.calculateGasPrice(ctx, options)
		if err != nil {
			return nil, err
		}

		options.GasPrice = gasPrice
	}

	fee := decimal.NewFromBigInt(new(big.Int).Mul(options.GasPrice, big.NewInt(int64(options.GasLimit))), 0)
	fromAddress := common.HexToAddress(w.normalizeAddress(w.wallet.Address))
	toAddress := common.HexToAddress(w.normalizeAddress(tx.ToAddress))
	contractAddress := common.HexToAddress(w.normalizeAddress(w.ContractAddress()))
	amount := w.ConvertToBaseUnit(tx.Amount)

	abiJSON, err := abi.JSON(strings.NewReader(abiDefinition))
	if err != nil {
		return nil, err
	}

	data, err := abiJSON.Pack("transfer", toAddress, amount.BigInt())
	if err != nil {
		return nil, err
	}

	nonce, err := w.client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return nil, err
	}

	chainID, err := w.client.NetworkID(context.Background())
	if err != nil {
		return nil, err
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(w.wallet.Secret, "0x"))
	if err != nil {
		return nil, err
	}

	signedTx, err := types.SignTx(
		types.NewTx(&types.LegacyTx{
			Gas:      options.GasLimit,
			GasPrice: options.GasPrice,
			Nonce:    nonce,
			To:       &contractAddress,
			Data:     data,
		}),
		types.NewEIP155Signer(chainID),
		privateKey,
	)

	if err := w.client.SendTransaction(ctx, signedTx); err != nil {
		return nil, err
	}

	tx.Fee = decimal.NewNullDecimal(w.ConvertFromBaseUnit(fee))
	tx.Status = transaction.StatusPending
	tx.TxHash = null.StringFrom(signedTx.Hash().Hex())

	return tx, nil
}

func (w *Wallet) normalizeAddress(address string) string {
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}

	return strings.ToLower(address)
}

func (w *Wallet) ContractAddress() string {
	if w.currency.Options["erc20_contract_address"] != nil {
		return w.currency.Options["erc20_contract_address"].(string)
	} else {
		return ""
	}
}

func (w *Wallet) calculateGasPrice(ctx context.Context, options Options) (*big.Int, error) {
	gasPrice, err := w.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	var rate float64
	switch options.GasRate {
	case wallet.GasPriceRateFast:
		rate = 1.1
	default:
		rate = 1
	}

	gasPrice = decimal.NewFromBigInt(gasPrice, 0).Mul(decimal.NewFromFloat(rate)).BigInt()

	return gasPrice, err
}

func (w *Wallet) LoadBalance(ctx context.Context) (balance decimal.Decimal, err error) {
	if len(w.ContractAddress()) > 0 {
		return w.loadBalanceErc20Balance(ctx, w.wallet.Address)
	} else {
		return w.loadBalanceEvmBalance(ctx, w.wallet.Address)
	}
}

func (w *Wallet) loadBalanceEvmBalance(ctx context.Context, address string) (balance decimal.Decimal, err error) {
	result, err := w.client.BalanceAt(ctx, common.HexToAddress(address), nil)
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromBigInt(result, -w.currency.Subunits), nil
}

func (w *Wallet) loadBalanceErc20Balance(ctx context.Context, address string) (balance decimal.Decimal, err error) {
	abiJSON, err := abi.JSON(strings.NewReader(abiDefinition))
	if err != nil {
		return decimal.Zero, err
	}

	data, err := abiJSON.Pack("balanceOf", common.HexToAddress(address))
	if err != nil {
		return decimal.Zero, err
	}

	contractAddress := common.HexToAddress(w.normalizeAddress(w.ContractAddress()))
	result, err := w.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return decimal.Zero, err
	}

	hex := hexutil.Encode(result)
	hex = "0x" + strings.TrimLeft(strings.TrimLeft(hex, "0x"), "0")

	if hex == "0x" {
		return decimal.Zero, nil
	}

	b, err := hexutil.DecodeBig(hex)
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromBigInt(b, -w.currency.Subunits), nil
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

	if options.GasPrice == nil {
		options.GasPrice = big.NewInt(0)
	}

	return options
}

func (w *Wallet) ConvertToBaseUnit(amount decimal.Decimal) decimal.Decimal {
	return amount.Mul(decimal.NewFromInt(int64(math.Pow10(int(w.currency.Subunits)))))
}

func (w *Wallet) ConvertFromBaseUnit(amount decimal.Decimal) decimal.Decimal {
	return amount.Div(decimal.NewFromInt(int64(math.Pow10(int(w.currency.Subunits)))))
}
